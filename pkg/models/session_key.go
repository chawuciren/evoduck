package models

import "fmt"

// BuildSessionKey 构建会话键
// 所有渠道统一使用 channel:userID 格式，确保 UserID 可被 extractUserID 正确解析
// 微信个人号：一个 bot 对应一个配置用户，SenderID 已是配置的 userID
// 其他渠道：channel:senderID
func BuildSessionKey(msg *NormalizedMessage) string {
	userID := msg.UserID
	if userID == "" {
		userID = msg.SenderID
	}
	if userID == "" {
		// 兜底：使用 AccountID 确保 session key 唯一
		userID = msg.AccountID
	}
	return fmt.Sprintf("%s:%s", msg.Channel, userID)
}

// BuildThreadKey 构建线程键（用于多轮对话）
func BuildThreadKey(msg *NormalizedMessage) string {
	if msg.ThreadID != "" {
		return msg.ThreadID
	}
	return BuildSessionKey(msg)
}
