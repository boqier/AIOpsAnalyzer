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
)

// AIOpsAnalyzerReconciler reconciles a AIOpsAnalyzer object
type AIOpsAnalyzerReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// 常量定义
const (
	prometheusQueryEndpoint = "http://prometheus-k8s.monitoring.svc.cluster.local:9090/api/v1/query"
	lokiQueryEndpoint       = "http://loki-gateway.monitoring.svc.cluster.local:3100/loki/api/v1/query"
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
		return ctrl.Result{}, err
	}

	// 5. 处理event string（根据您的业务逻辑）
	log.Info("成功构建event string", "length", len(eventString))
	// 可以将eventString用于后续的AI分析、通知等

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

	// 构建Loki查询
	query := fmt.Sprintf("{namespace='%s'}", target.Namespace)
	if target.Selector.MatchLabels != nil {
		for k, v := range target.Selector.MatchLabels {
			query += fmt.Sprintf(",%s='%s'", k, v)
		}
	}
	query += " |= \"error\" or |= \"ERROR\" or |= \"panic\" or |= \"PANIC\""

	// 发送请求
	timeRange := time.Now().Add(-5*time.Minute).UnixNano() / int64(time.Millisecond)
	resp, err := http.Get(fmt.Sprintf("%s?query=%s&time=%d", lokiQueryEndpoint, url.QueryEscape(query), timeRange))
	if err != nil {
		log.Error(err, "发送Loki查询请求失败")
		return "", err
	}
	defer resp.Body.Close()

	// 解析响应
	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		log.Error(err, "解析Loki响应失败")
		return "", err
	}

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
