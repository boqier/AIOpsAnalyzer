package xixi

import (
	"context"
	"errors"

	"github.com/sashabaranov/go-openai"
)

type OpenAI struct {
	Client *openai.Client
	ctx    context.Context
}

func NewOpenAIClient() (*OpenAI, error) {
	apiKey := "sk-lqcuebxcbfrtrwlckktalpvvsnwxomdneswvuhytfqoookrw"
	config := openai.DefaultConfig(apiKey)
	config.BaseURL = "https://api.siliconflow.cn/v1"
	client := openai.NewClientWithConfig(config)

	ctx := context.Background()

	return &OpenAI{
		Client: client,
		ctx:    ctx,
	}, nil
}

// SendMessage 发送消息到 LLM 并返回原始字符串响应
func (o *OpenAI) SendMessage(content string) (string, error) {
	prompt := `你是一个拥有 10 年 Kubernetes 生产运维经验的资深 SRE，目前负责一个严格使用 ArgoCD + Kustomize + GitOps 的集群。
你正在执行全自动 AIOps 自愈闭环，你只能通过生成 JSON 6902 Patch + target 选择器来修改资源，禁止任何其他方式。

### 严格要求（必须 100% 遵守，否则自愈失败）：
1. 只能使用 RFC6902 JSON Patch 格式
2. 必须使用 target + labelSelector 定位资源，严禁写死 metadata.name
3. 只允许修改 Deployment、StatefulSet、HorizontalPodAutoscaler
4. 扩容时必须同时提升 requests 和 limits，防止 CPU Throttling
5. 所有数值必须是合理生产值（replicas ≤ 100，CPU ≤ 8，内存 ≤ 16Gi）
6. patch_file 字段必须使用当前真实时间戳 + 简短英文描述，格式严格为：YYYYMMDD-HHMMSS-short-desc.yaml
   - 当前时间（北京时间）：20251126-204733
   - 示例：20251126-204733-cpu-spike.yaml
7. 输出必须是合法的 JSON，禁止任何解释、markdown、换行符外的文字`
	req := openai.ChatCompletionRequest{
		Model: "Qwen/Qwen2.5-72B-Instruct",
		Messages: []openai.ChatCompletionMessage{
			{
				Role:    "system",
				Content: prompt,
			},
			{
				Role:    "user",
				Content: content,
			},
		},
	}

	resp, err := o.Client.CreateChatCompletion(o.ctx, req)
	if err != nil {
		return "", err
	}

	if len(resp.Choices) == 0 {
		return "", errors.New("no response from OpenAI")
	}

	return resp.Choices[0].Message.Content, nil
}
