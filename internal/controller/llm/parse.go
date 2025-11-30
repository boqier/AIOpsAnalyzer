package llm

import (
	"encoding/json"
	"fmt"
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

// ---------- 主解析逻辑 ----------
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
