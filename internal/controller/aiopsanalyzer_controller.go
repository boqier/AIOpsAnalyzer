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

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
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

	// 4. 处理获取到的Pod列表（根据您的业务逻辑）
	for _, pod := range targetPods {
		log.Info("处理Pod", "name", pod.Name, "namespace", pod.Namespace, "labels", pod.Labels)
		// 执行您的业务逻辑，例如分析Pod状态、获取指标等
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

//发送飞书请求
