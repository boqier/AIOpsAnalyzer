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

package v1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=aia;aiops
// +kubebuilder:printcolumn:name="App",type=string,JSONPath=`.spec.target.selector.matchLabels.app`
// +kubebuilder:printcolumn:name="Namespace",type=string,JSONPath=`.spec.target.namespace`
// +kubebuilder:printcolumn:name="Status",type=string,JSONPath=`.status.summary`
// +kubebuilder:printcolumn:name="PR",type=string,JSONPath=`.status.gitOps.pr.number`,priority=10
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// AIOpsAnalyzer 是 AI 驱动的 GitOps 自动修复资源
type AIOpsAnalyzer struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   AIOpsAnalyzerSpec   `json:"spec,omitempty"`
	Status AIOpsAnalyzerStatus `json:"status,omitempty"`
}

// ==================== Spec ====================

type AIOpsAnalyzerSpec struct {
	// 监控目标
	// +kubebuilder:validation:Required
	Target TargetSelector `json:"target"`

	// 分析周期
	// +kubebuilder:default="5m"
	// +kubebuilder:validation:Pattern=`^(\d+m|\d+h|\d+s)$`
	AnalysisInterval string `json:"analysisInterval,omitempty"`

	// 飞书通知与审批配置
	// +kubebuilder:validation:Required
	Feishu FeishuNotification `json:"feishu"`

	// GitOps 配置
	// +kubebuilder:validation:Required
	GitOps GitOpsConfig `json:"gitOps"`

	// 自动修复策略
	AutoRemediation AutoRemediationSpec `json:"autoRemediation,omitempty"`

	// 阈值配置（可选，AI 可覆盖）
	Thresholds *Thresholds `json:"thresholds,omitempty"`
}

type TargetSelector struct {
	Namespace string               `json:"namespace,omitempty"`
	Selector  metav1.LabelSelector `json:"selector"`
}

type FeishuNotification struct {
	// 接收消息的类型和 ID（支持私聊、群聊、指定人）
	// +kubebuilder:validation:Required
	ReceiveIDType FeishuReceiveIDType `json:"receiveIdType"`
	// +kubebuilder:validation:Required
	ReceiveID string `json:"receiveId"`

	// 可选：@指定的审批人（支持多个）
	MentionUsers []string `json:"mentionUsers,omitempty"`
	MentionRoles []string `json:"mentionRoles,omitempty"` // 如 "oncall-sre"

	// 审批超时时间
	// +kubebuilder:default="10m"
	ApprovalTimeout string `json:"approvalTimeout,omitempty"`
}

// +kubebuilder:validation:Enum=user_id;open_id;union_id;user_open_id;chat_id;email
type FeishuReceiveIDType string

const (
	FeishuUserID  FeishuReceiveIDType = "user_id"
	FeishuOpenID  FeishuReceiveIDType = "open_id"
	FeishuUnionID FeishuReceiveIDType = "union_id"
	FeishuChatID  FeishuReceiveIDType = "chat_id"
	FeishuEmail   FeishuReceiveIDType = "email"
)

type GitOpsConfig struct {
	// Git 仓库地址（支持 https 和 ssh）
	// +kubebuilder:validation:Required
	RepoURL string `json:"repoURL"`

	// 分支
	// +kubebuilder:default="main"
	Branch string `json:"branch,omitempty"`

	// 应用在仓库中的路径
	// +kubebuilder:validation:Required
	Path string `json:"path"`

	// Git 认证 Secret（包含 token 或 ssh key）
	// +kubebuilder:validation:Required
	TokenSecretRef corev1.LocalObjectReference `json:"tokenSecretRef"`

	// 可选：提交者信息
	CommitAuthorName  string `json:"commitAuthorName,omitempty"`
	CommitAuthorEmail string `json:"commitAuthorEmail,omitempty"`
}

type AutoRemediationSpec struct {
	// 是否启用自动修复
	// +kubebuilder:default=true
	Enabled bool `json:"enabled,omitempty"`

	// 是否需要飞书审批
	// +kubebuilder:default=true
	RequireApproval bool `json:"requireApproval,omitempty"`

	// 允许的修复类型（可多选）
	// +kubebuilder:validation:ItemsEnum=scale;restart;config;traffic;resource;feature-toggle
	AllowedActions []string `json:"allowedActions,omitempty"`
}

type Thresholds struct {
	CPU               string `json:"cpu,omitempty"`
	Memory            string `json:"memory,omitempty"`
	RestartCount      *int32 `json:"restartCount,omitempty"`
	ErrorLogPerMinute *int32 `json:"errorLogPerMinute,omitempty"`
}

// ==================== Status ====================

type AIOpsAnalyzerStatus struct {
	// 最近分析时间
	LastAnalysisTime *metav1.Time `json:"lastAnalysisTime,omitempty"`

	// 简要状态
	// +kubebuilder:default="Healthy"
	Summary string `json:"summary,omitempty"`

	// AI 分析结论
	Insights string `json:"insights,omitempty"`
	// AI patch补丁
	ProposedRemediation *RemediationProposal `json:"proposedRemediation,omitempty"`
	// 当前待审批请求
	PendingApproval *ApprovalRequest `json:"pendingApproval,omitempty"`

	// GitOps PR 状态
	GitOps GitOpsStatus `json:"gitOps,omitempty"`

	// 标准字段
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
}

type RemediationProposal struct {
	// AI 建议执行的动作类型
	// +kubebuilder:validation:Enum=scale;restart;feature-toggle;traffic-shift;resource-adjust;config-change
	ActionType string `json:"actionType"`

	// 结构化的补丁内容（Operator 直接序列化成 YAML 提交 Git）
	Patches []PatchOperation `json:"patches"`

	// AI 给出的理由（给人看 + 写进 PR 描述）
	Reason string `json:"reason"`

	// 建议的紧急程度
	// +kubebuilder:validation:Enum=low;medium;high;critical
	Severity string `json:"severity,omitempty"`

	// 生成时间
	GeneratedAt metav1.Time `json:"generatedAt"`
}

// 单个 patch 操作（完全对应 Kubernetes Patch API）
type PatchOperation struct {
	// Patch 类型
	// +kubebuilder:validation:Enum=replace;add;remove
	Op        string                  `json:"op"`
	TargetRef *corev1.ObjectReference `json:"targetRef,omitempty"`
	// JSON Path（如 /spec/replicas）
	Path string `json:"path"`

	// 新值（任意类型，json.Marshal 后提交）
	Value runtime.RawExtension `json:"value"`
}

type ApprovalRequest struct {
	// 唯一请求 ID（用于飞书回调匹配）
	RequestID string `json:"requestID"`

	// 飞书消息 ID（用于更新卡片）
	MessageID string `json:"messageID,omitempty"`

	// 请求时间与过期时间
	RequestedAt metav1.Time `json:"requestedAt"`
	ExpiresAt   metav1.Time `json:"expiresAt"`

	// 审批状态
	Approved   *bool  `json:"approved,omitempty"`
	ApprovedBy string `json:"approvedBy,omitempty"`
	Reason     string `json:"reason,omitempty"`
}

type GitOpsStatus struct {
	PR PRStatus `json:"pr,omitempty"`

	// 最后一次提交的 commit hash
	LastCommitSHA string `json:"lastCommitSHA,omitempty"`

	// 最后同步时间（ArgoCD 同步后可通过 event 更新）
	LastSyncedTime *metav1.Time `json:"lastSyncedTime,omitempty"`
}

type PRStatus struct {
	Number   int          `json:"number,omitempty"`
	URL      string       `json:"url,omitempty"`
	Status   string       `json:"status,omitempty"` // draft / open / merged / closed
	Merged   bool         `json:"merged,omitempty"`
	MergedAt *metav1.Time `json:"mergedAt,omitempty"`
}

// +kubebuilder:object:root=true

// AIOpsAnalyzerList contains a list of AIOpsAnalyzer.
type AIOpsAnalyzerList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []AIOpsAnalyzer `json:"items"`
}

func init() {
	SchemeBuilder.Register(&AIOpsAnalyzer{}, &AIOpsAnalyzerList{})
}
