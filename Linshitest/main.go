package xixi

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	lark "github.com/larksuite/oapi-sdk-go/v3"
)

type PatchOp struct {
	Op    string `json:"op"`
	Path  string `json:"path"`
	Value any    `json:"value"` // 支持 int、string、object 等任意类型
}

type Target struct {
	Kind          string `json:"kind"`
	LabelSelector string `json:"labelSelector"`
}

// heal 时的完整结构体
type HealAction struct {
	Namespace         string    `json:"namespace"`
	Action            string    `json:"action"` // 一定是 "heal"
	Reason            string    `json:"reason"`
	Detail            string    `json:"detail"`
	PatchFile         string    `json:"patch_file"`
	PatchContent      []PatchOp `json:"patch_content"`
	Target            Target    `json:"target"`
	SuggestedDuration string    `json:"suggested_duration"`
	RiskLevel         string    `json:"risk_level"`
}

// noop 时的结构体（只有两个字段）
type NoopAction struct {
	Action string `json:"action"` // 一定是 "noop"
	Reason string `json:"reason"`
}

// ---------- 通用的解析函数 ----------
func ParseJSONTo(jsonStr string, target any) error {
	if err := json.Unmarshal([]byte(jsonStr), target); err != nil {
		return fmt.Errorf("json unmarshal failed: %w", err)
	}
	return nil
}

// ---------- 主解析逻辑（推荐这样写）----------
func ParseAutoHealResponse(jsonStr string) (any, error) {
	// 第一步：先只解析 action 和 reason，判断是哪种响应
	type base struct {
		Action string `json:"action"`
		Reason string `json:"reason"`
	}

	var b base
	if err := json.Unmarshal([]byte(jsonStr), &b); err != nil {
		return nil, fmt.Errorf("parse base failed: %w", err)
	}

	switch b.Action {
	case "heal":
		var heal HealAction
		if err := ParseJSONTo(jsonStr, &heal); err != nil {
			return nil, err
		}
		// 可选：在这里做严格校验
		if heal.RiskLevel != "low" && heal.RiskLevel != "medium" && heal.RiskLevel != "high" {
			return nil, fmt.Errorf("invalid risk_level: %s", heal.RiskLevel)
		}
		return &heal, nil

	case "noop":
		var noop NoopAction
		if err := ParseJSONTo(jsonStr, &noop); err != nil {
			return nil, err
		}
		return &noop, nil

	default:
		return nil, fmt.Errorf("unknown action: %s", b.Action)
	}
}

func main() {
	client := lark.NewClient("cli_a9a95e30b7f85bc9", "1tzulFiDFgLlw3AbR3eCQeYZRl08g0Xs")
	ctx := context.Background()
	llm, err := NewOpenAIClient()
	if err != nil {
		log.Fatal(err)
	}
	currentTime := time.Now().Format("20060102-150405")
	content := fmt.Sprintf(`### 当前应用信息（请原样使用）：
- 应用标签选择器：app.kubernetes.io/name=order-service
- 命名空间：order-prod
- 当前副本数：10
- 当前 CPU limits：2000m
- 当前 CPU requests：1000m
- 当前内存 limits：4Gi
- 当前时间: %s

### 告警/监控数据：
【紧急告警】CPUUsageHigh 已持续 18 分钟
当前 CPU 使用率：99%（阈值 80%）
CPU Throttling 时间占比：78%（过去 15 分钟）
QPS 从 1800 突增至 9200（增长 411%）
HPA 目标已到 maxReplicas=30，但因为单 Pod CPU 配额不足导致无法调度新 Pod
有 7 个 Pod 持续处于 Pending 状态，节点 CPU 资源耗尽

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
}`, currentTime)

	response, err := llm.SendMessage(content)
	if err != nil {
		log.Fatal(err)
	}
	for _, s := range []string{response} {
		result, err := ParseAutoHealResponse(s)
		if err != nil {
			log.Fatal(err)
		}

		switch v := result.(type) {
		//改为创建审批请求
		case *HealAction:
			fmt.Println("自愈动作")
			fmt.Println("原因:", v.Reason)
			fmt.Println("风险:", v.RiskLevel)
			fmt.Println("补丁文件:", v.PatchFile)

			// 构造卡片变量
			cardMsg := CreateHealActionCardMessage(
				v,
				"oc_fb3b64606e33897e741fc355529e5784", // 接收者ID
				"chat_id",                             // 接收类型
				"AAqhGHg0Wgux8",                       // 模板ID
				"0.0.6",                               // 模板版本
			)

			// 发送卡片
			err := SendTemplateCard(ctx, client, cardMsg)
			if err != nil {
				log.Printf("发送卡片失败: %v", err)
			} else {
				log.Println("卡片发送成功")
			}
		case *NoopAction:
			//更新status，然后return
			fmt.Println("无需操作:", v.Reason)
		}
		fmt.Println("---")
	}
}
