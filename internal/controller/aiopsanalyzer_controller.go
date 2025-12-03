/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	yaml "k8s.io/apimachinery/pkg/runtime/serializer/json"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	autofixv1 "github.com/boqier/AIOpsAnalyzer/api/v1"
	"github.com/boqier/AIOpsAnalyzer/internal/controller/feishu"
	"github.com/boqier/AIOpsAnalyzer/internal/controller/llm"
	lark "github.com/larksuite/oapi-sdk-go/v3"
)

// AIOpsAnalyzerReconciler reconciles a AIOpsAnalyzer object
type AIOpsAnalyzerReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// 常量定义
const (
	prometheusQueryEndpoint = "http://127.0.0.1:9090/api/v1/query"
	lokiQueryEndpoint       = "http://127.0.0.1:3100/loki/api/v1/query"
)

// +kubebuilder:rbac:groups=autofix.aiops.com,resources=aiopsanalyzers,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=autofix.aiops.com,resources=aiopsanalyzers/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=autofix.aiops.com,resources=aiopsanalyzers/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the AIOpsAnalyzer object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.19.1/pkg/reconcile
func (r *AIOpsAnalyzerReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx)
	// 1. 获取AIOpsAnalyzer实例
	var aiopsAnalyzer autofixv1.AIOpsAnalyzer
	if err := r.Get(ctx, req.NamespacedName, &aiopsAnalyzer); err != nil {
		log.Error(err, "获取AIOpsAnalyzer资源失败")
		return ctrl.Result{}, err
	}

	// 2. 检查是否有TargetSelector配置
	if aiopsAnalyzer.Spec.Target.Selector.MatchLabels == nil && aiopsAnalyzer.Spec.Target.Selector.MatchExpressions == nil {
		log.Info("未配置TargetSelector，跳过Pod获取")
		return ctrl.Result{}, nil
	}

	// 3. 直接使用GetTargetPods函数获取匹配的Pod列表
	targetPods, err := r.GetTargetPods(ctx, &aiopsAnalyzer.Spec.Target)
	if err != nil {
		log.Error(err, "获取目标Pod失败")
		return ctrl.Result{}, err
	}

	log.Info("成功获取匹配的Pod", "count", len(targetPods))

	// 4. 构建event string
	eventString, err := r.BuildEventString(ctx, &aiopsAnalyzer.Spec.Target)
	if err != nil {
		log.Error(err, "构建event string失败")
		return ctrl.Result{RequeueAfter: 10 * time.Second}, err
	}

	// 5. 处理event string（根据您的业务逻辑）
	log.Info("成功构建event string", "length", len(eventString))
	log.Info("event string内容", "content", eventString)

	// 6. 调用大模型生成修复方案
	llmClient, err := llm.NewOpenAIClient()
	if err != nil {
		log.Error(err, "创建大模型客户端失败")
		return ctrl.Result{}, err
	}

	// 构建大模型请求内容
	currentTime := time.Now().Format("20060102-150405")
	content := fmt.Sprintf(`### 当前应用信息（请原样使用）：
- 应用标签选择器：app.kubernetes.io/name=order-service
- 命名空间：product-a
- 当前副本数：1
- 当前 CPU limits：2000m
- 当前 CPU requests：1000m
- 当前内存 limits：4Gi
- 当前时间: %s

### 告警/监控数据：
%s

请立即决定是否需要自愈，如果需要，按以下 JSON 格式输出（只能输出这个 JSON）：

{
  "action": "heal" | "noop",
  "namespace": "order-prod",
  "reason": "一句话中文原因，用于 git commit（≤50字）",
  "detail": "详细技术说明，包含问题说明，以及解决方案简述，用于 PR body（≤300字）",
  "patch_file": "20251126-204555-cpu-spike.yaml",
  "patch_content": [
    {
      "op": "replace",
      "path": "/spec/replicas",
      "value": 20
    }
  ],
  "target": {
    "kind": "Deployment",
    "labelSelector": "app.kubernetes.io/name=order-service"
  },
  "suggested_duration": "30m",
  "risk_level": "low" | "medium" | "high"
}

如果不需要自愈，输出：
{
  "action": "noop",
  "reason": "当前指标正常，无需干预"
}`, currentTime, eventString)

	response, err := llmClient.SendMessage(content)
	if err != nil {
		log.Error(err, "调用大模型失败")
		return ctrl.Result{}, err
	}

	// 7. 解析大模型响应
	result, err := llm.ParseAutoHealResponse(response)
	if err != nil {
		log.Error(err, "解析大模型响应失败")
		return ctrl.Result{}, err
	}

	// 8. 根据响应类型执行不同操作
	switch v := result.(type) {
	case *llm.HealAction:
		log.Info("自愈动作")
		log.Info("原因:", "reason", v.Reason)
		log.Info("风险:", "risk_level", v.RiskLevel)
		log.Info("补丁文件:", "patch_file", v.PatchFile)

		// 9. 构造卡片变量并发送卡片
		// 初始化飞书客户端（暂时使用硬编码值，后续可从配置或Secret中获取）
		client := lark.NewClient("cli_a9a95e30b7f85bc9", "1tzulFiDFgLlw3AbR3eCQeYZRl08g0Xs")

		// 将 []llm.PatchOp 转换为 []feishu.PatchOp
		patches := make([]feishu.PatchOp, len(v.PatchContent))
		for i, op := range v.PatchContent {
			patches[i] = feishu.PatchOp{
				Op:    op.Op,
				Path:  op.Path,
				Value: op.Value,
			}
		}

		// 构造卡片变量
		cardMsg := feishu.NewCardMessage(
			aiopsAnalyzer.Spec.Feishu.ReceiveID,             // 接收者ID
			string(aiopsAnalyzer.Spec.Feishu.ReceiveIDType), // 接收类型
			"AAqhGHg0Wgux8", // 模板ID（暂时硬编码）
			"0.0.9",         // 模板版本（暂时硬编码）
			&feishu.CardVariables{
				Reason:          v.Reason,
				Patch:           fmt.Sprintf("%v", v.PatchContent),
				Patches:         patches,
				ResolveFunction: v.Detail,
				Namespace:       v.Namespace,
				Name:            v.Target.LabelSelector,
				RequestID:       fmt.Sprintf("%s-%d", v.PatchFile, time.Now().Unix()),
			},
		)

		// 发送卡片
		err := feishu.SendTemplateCard(ctx, client, cardMsg)
		if err != nil {
			log.Error(err, "发送卡片失败")
		} else {
			log.Info("卡片发送成功")
		}
	case *llm.NoopAction:
		// 更新status，然后return
		log.Info("无需操作:", "reason", v.Reason)
	}

	return ctrl.Result{}, nil
}

// GetTargetPods 根据TargetSelector获取对应的Pod列表
func (r *AIOpsAnalyzerReconciler) GetTargetPods(ctx context.Context, target *autofixv1.TargetSelector) ([]corev1.Pod, error) {
	log := log.FromContext(ctx)

	// 处理命名空间
	namespace := target.Namespace
	if namespace == "" {
		namespace = corev1.NamespaceDefault
		log.V(1).Info("未指定命名空间，使用默认命名空间", "namespace", namespace)
	}

	// 创建 ListOptions
	listOptions := &client.ListOptions{
		Namespace: namespace,
	}
	if target.Selector.MatchLabels != nil || target.Selector.MatchExpressions != nil {
		selector, err := metav1.LabelSelectorAsSelector(&target.Selector)
		if err != nil {
			log.Error(err, "无法将 LabelSelector 转换为 Selector", "selector", target.Selector)
			return nil, err
		}
		listOptions.LabelSelector = selector
		log.V(1).Info("应用标签选择器", "selector", selector.String())
	} else {
		log.V(1).Info("未配置标签选择器，将获取命名空间内所有 Pod")
	}

	// 执行列表查询
	var pods corev1.PodList
	if err := r.List(ctx, &pods, listOptions); err != nil {
		log.Error(err, "获取Pod列表失败", "namespace", namespace, "selector", target.Selector)
		return nil, err
	}

	log.Info("成功获取目标Pod", "count", len(pods.Items), "namespace", namespace, "selector", target.Selector)
	return pods.Items, nil
}

// BuildLabelSelector 根据标签构建LabelSelector，测试使用
func BuildLabelSelector(labels map[string]string) (*metav1.LabelSelector, error) {
	matchLabels := make(map[string]string)
	for k, v := range labels {
		matchLabels[k] = v
	}

	return &metav1.LabelSelector{
		MatchLabels: matchLabels,
	}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *AIOpsAnalyzerReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&autofixv1.AIOpsAnalyzer{}).
		Named("aiopsanalyzer").
		Complete(r)
}

// GetTargetResourceYAML 根据TargetSelector获取资源YAML并过滤不重要的字段
func (r *AIOpsAnalyzerReconciler) GetTargetResourceYAML(ctx context.Context, target *autofixv1.TargetSelector) (string, error) {
	log := log.FromContext(ctx)

	// 1. 获取目标Pod列表
	pods, err := r.GetTargetPods(ctx, target)
	if err != nil {
		log.Error(err, "获取目标Pod失败")
		return "", err
	}

	if len(pods) == 0 {
		return "", nil
	}

	// 2. 过滤Pod字段
	filteredPods := make([]corev1.Pod, len(pods))
	for i, pod := range pods {
		filteredPods[i] = *FilterPodFields(&pod)
	}

	// 3. 序列化为YAML
	serializer := yaml.NewSerializerWithOptions(yaml.DefaultMetaFactory, nil, nil, yaml.SerializerOptions{
		Yaml:   true,
		Pretty: true,
	})

	var yamlBuilder strings.Builder
	for _, pod := range filteredPods {
		err := serializer.Encode(&pod, &yamlBuilder)
		if err != nil {
			log.Error(err, "序列化Pod为YAML失败", "podName", pod.Name)
			continue
		}
		yamlBuilder.WriteString("---\n")
	}

	return yamlBuilder.String(), nil
}

// FilterPodFields 过滤Pod中不重要的字段
func FilterPodFields(pod *corev1.Pod) *corev1.Pod {
	// 创建Pod副本以避免修改原始对象
	filtered := pod.DeepCopy()

	// 过滤metadata中的字段
	filtered.ObjectMeta.ManagedFields = nil
	filtered.ObjectMeta.ResourceVersion = ""
	filtered.ObjectMeta.UID = ""
	filtered.ObjectMeta.CreationTimestamp = metav1.Time{}
	filtered.ObjectMeta.Generation = 0
	filtered.ObjectMeta.Finalizers = nil
	filtered.ObjectMeta.OwnerReferences = nil

	// 过滤status中的字段
	filtered.Status = corev1.PodStatus{
		Phase: filtered.Status.Phase,
		Conditions: []corev1.PodCondition{
			{
				Type:   corev1.PodReady,
				Status: filtered.Status.Conditions[len(filtered.Status.Conditions)-1].Status,
			},
		},
		ContainerStatuses: []corev1.ContainerStatus{
			{
				Name:  filtered.Status.ContainerStatuses[0].Name,
				Ready: filtered.Status.ContainerStatuses[0].Ready,
				State: filtered.Status.ContainerStatuses[0].State,
			},
		},
	}

	return filtered
}

// GetPrometheusAlerts 从Prometheus获取告警信息
func (r *AIOpsAnalyzerReconciler) GetPrometheusAlerts(ctx context.Context, target *autofixv1.TargetSelector) (string, error) {
	log := log.FromContext(ctx)

	// 构建Prometheus查询
	query := fmt.Sprintf("ALERTS{namespace='%s'}", target.Namespace)
	if target.Selector.MatchLabels != nil {
		for k, v := range target.Selector.MatchLabels {
			query += fmt.Sprintf(",%s='%s'", k, v)
		}
	}
	query += " and ALERTS.state='firing'"

	// 发送请求
	resp, err := http.Get(fmt.Sprintf("%s?query=%s", prometheusQueryEndpoint, url.QueryEscape(query)))
	if err != nil {
		log.Error(err, "发送Prometheus查询请求失败")
		return "", err
	}
	defer resp.Body.Close()

	// 解析响应
	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		log.Error(err, "解析Prometheus响应失败")
		return "", err
	}

	// 格式化告警信息
	var alertsBuilder strings.Builder
	if data, ok := result["data"].(map[string]interface{}); ok {
		if resultType, ok := data["resultType"].(string); ok && resultType == "vector" {
			if results, ok := data["result"].([]interface{}); ok {
				for _, item := range results {
					if alert, ok := item.(map[string]interface{}); ok {
						if metric, ok := alert["metric"].(map[string]interface{}); ok {
							alertsBuilder.WriteString(fmt.Sprintf("Alert: %s\n", metric["alertname"]))
							alertsBuilder.WriteString(fmt.Sprintf("  Namespace: %s\n", metric["namespace"]))
							if pod, ok := metric["pod"].(string); ok {
								alertsBuilder.WriteString(fmt.Sprintf("  Pod: %s\n", pod))
							}
							alertsBuilder.WriteString("\n")
						}
					}
				}
			}
		}
	}

	return alertsBuilder.String(), nil
}

// GetLokiLogs 从Loki获取日志信息
func (r *AIOpsAnalyzerReconciler) GetLokiLogs(ctx context.Context, target *autofixv1.TargetSelector) (string, error) {
	log := log.FromContext(ctx)

	// 构建 LogQL 查询：关键修复点是将所有标签值从单引号 ' 更改为双引号 "
	query := fmt.Sprintf("{namespace=\"%s\"", target.Namespace)
	log.Info("查询命名空间", "namespace", target.Namespace)

	if target.Selector.MatchLabels != nil {
		for k, v := range target.Selector.MatchLabels {
			// 使用双引号 " 包裹标签值
			query += fmt.Sprintf(",%s=\"%s\"", k, v)
		}
	}
	// 正则表达式部分保持不变，使用反引号 `
	// 直接用 or 连接多个字面量匹配（大小写分开写，覆盖所有常见变体）
	query += "} |~ \"(?i)(error|panic|fatal|critical)\""

	// 这一行计算的是毫秒时间戳
	timeRange := time.Now().Add(-48*time.Minute).UnixNano() / int64(time.Millisecond)
	log.Info("查询起始时间", "timeRange", time.Now().Add(-48*time.Minute).Format("2006-01-02 15:04:05"))
	log.Info("query 语句", "query", query)
	log.Info("查询时间范围", "timeRange", timeRange)
	// 对完整的 LogQL query 进行 URL 编码
	url := fmt.Sprintf("%s?query=%s&start=%d", lokiQueryEndpoint, url.QueryEscape(query), timeRange)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return "", err
	}

	// 关键行：设置 X-Scope-OrgID header
	req.Header.Set("X-Scope-OrgID", "1")

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		log.Error(err, "发送Loki查询请求失败")
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		log.Error(nil, "Loki返回非200", "status", resp.StatusCode, "body", string(body))
		return "", fmt.Errorf("loki returned %d: %s", resp.StatusCode, string(body))
	}

	// 注意：这里打印 resp.Body 是错误的，因为它是一个 io.ReadCloser，需要先读取才能打印内容
	// 但为了保持原意，我们继续往下解析。
	log.Info("Loki查询响应", "status", resp.StatusCode)

	// 解析响应
	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		log.Error(err, "解析Loki响应失败")
		return "", err
	}
	log.Info("Loki查询响应", "result", result)
	// 格式化日志信息
	var logsBuilder strings.Builder
	if data, ok := result["data"].(map[string]interface{}); ok {
		if resultType, ok := data["resultType"].(string); ok && resultType == "streams" {
			if streams, ok := data["result"].([]interface{}); ok {
				for _, stream := range streams {
					if streamData, ok := stream.(map[string]interface{}); ok {
						if values, ok := streamData["values"].([]interface{}); ok {
							for _, value := range values {
								if logEntry, ok := value.([]interface{}); ok && len(logEntry) >= 2 {
									// logEntry[0] 是时间戳，logEntry[1] 是日志行内容
									logsBuilder.WriteString(fmt.Sprintf("%s: %s\n", logEntry[0], logEntry[1]))
								}
							}
						}
					}
				}
			}
		}
	}

	return logsBuilder.String(), nil
}

// BuildEventString 组装event string
func (r *AIOpsAnalyzerReconciler) BuildEventString(ctx context.Context, target *autofixv1.TargetSelector) (string, error) {
	log := log.FromContext(ctx)

	// 1. 获取资源YAML
	resourceYAML, err := r.GetTargetResourceYAML(ctx, target)
	if err != nil {
		log.Error(err, "获取资源YAML失败")
		return "", err
	}

	// 2. 获取Prometheus告警
	prometheusAlerts, err := r.GetPrometheusAlerts(ctx, target)
	if err != nil {
		log.Error(err, "获取Prometheus告警失败")
		return "", err
	}
	log.Info("Prometheus告警信息", "alerts", prometheusAlerts)
	// 3. 获取Loki日志
	lokiLogs, err := r.GetLokiLogs(ctx, target)
	if err != nil {
		log.Error(err, "获取Loki日志失败")
		return "", err
	}

	// 4. 组装event string
	var eventBuilder strings.Builder

	eventBuilder.WriteString("=== Target Resource Information ===\n")
	eventBuilder.WriteString(resourceYAML)

	eventBuilder.WriteString("\n=== Prometheus Alerts ===\n")
	if prometheusAlerts == "" {
		eventBuilder.WriteString("No firing alerts\n")
	} else {
		eventBuilder.WriteString(prometheusAlerts)
	}

	eventBuilder.WriteString("\n=== Loki Error Logs ===\n")
	if lokiLogs == "" {
		eventBuilder.WriteString("No error logs\n")
	} else {
		eventBuilder.WriteString(lokiLogs)
	}

	return eventBuilder.String(), nil
}

//发送飞书请求
