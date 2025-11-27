package main

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
)

// 方便后续不同的卡片模板变量
type CardVariables struct {
	ResolveFunction string `json:"resolve_fuction"`
	Namespace       string `json:"namespace"`
	Name            string `json:"name"`
	RequestID       string `json:"request_id"`
}

type CardMessage struct {
	ReceiveID   string // chat_id / open_id 等
	ReceiveType string // "chat_id"、"open_id"、"user_id" 等  ← 重点！
	TemplateID  string
	Version     string
	Variables   *CardVariables
}

// 推荐构造函数（一个就够全项目用）
func NewCardMessage(receiveID, receiveType, templateID, version string, vars *CardVariables) *CardMessage {
	return &CardMessage{
		ReceiveID:   receiveID,
		ReceiveType: receiveType,
		TemplateID:  templateID,
		Version:     version,
		Variables:   vars,
	}
}

// 创建修复动作卡片消息
// v 是包含Reason和Target信息的结构体指针
// receiveID 是接收者ID
// receiveType 是接收类型：chat_id、open_id或user_id
// templateID 是模板ID
// version 是模板版本
func CreateHealActionCardMessage(v interface{}, receiveID, receiveType, templateID, version string) *CardMessage {
	// 假设v是包含Reason和Target的结构体
	vValue := reflect.ValueOf(v).Elem()

	// 获取Reason字段
	reason := ""
	if reasonField := vValue.FieldByName("Reason"); reasonField.IsValid() && reasonField.Kind() == reflect.String {
		reason = reasonField.String()
	}

	// 获取Target字段及其Kind和LabelSelector
	targetKind := ""
	targetLabelSelector := ""
	if targetField := vValue.FieldByName("Target"); targetField.IsValid() && targetField.Kind() == reflect.Struct {
		if kindField := targetField.FieldByName("Kind"); kindField.IsValid() && kindField.Kind() == reflect.String {
			targetKind = kindField.String()
		}
		if selectorField := targetField.FieldByName("LabelSelector"); selectorField.IsValid() && selectorField.Kind() == reflect.String {
			targetLabelSelector = selectorField.String()
		}
	}
	fmt.Printf("targetKind: %s, targetLabelSelector: %s\n", targetKind, targetLabelSelector)
	// 构造卡片变量
	vars := &CardVariables{
		ResolveFunction: reason,
		Namespace:       "default",
		Name:            targetLabelSelector,
		RequestID:       "FEISHU-TEST-ABC-123",
	}

	// 创建卡片消息
	return NewCardMessage(receiveID, receiveType, templateID, version, vars)
}

// 最终正确的发送函数
func SendTemplateCard(ctx context.Context, client *lark.Client, msg *CardMessage) error {
	// 1. 正确生成 content（Variables 是结构体，json tag 自动生效）
	content, err := json.Marshal(map[string]any{
		"type": "template",
		"data": map[string]any{
			"template_id":           msg.TemplateID,
			"template_version_name": msg.Version,
			"template_variable":     msg.Variables, // 直接传结构体！不需要 map
		},
	})
	if err != nil {
		return fmt.Errorf("marshal card content failed: %w", err)
	}

	// 2. 正确使用 msg 里的字段
	req := larkim.NewCreateMessageReqBuilder().
		ReceiveIdType(msg.ReceiveType). // 正确：类型从结构体取
		Body(larkim.NewCreateMessageReqBodyBuilder().
			ReceiveId(msg.ReceiveID). // 正确：ID 从结构体取
			MsgType("interactive").
			Content(string(content)).
			Build()).
		Build()

	// 3. 新版 SDK 正确的调用方式（v3.0+）
	resp, err := client.Im.V1.Message.Create(ctx, req)
	if err != nil {
		return fmt.Errorf("send card message failed: %w", err)
	}
	if !resp.Success() {
		return fmt.Errorf("send card failed: code=%d, msg=%s, request_id=%s", resp.Code, resp.Msg, resp.RequestId())
	}

	return nil
}
