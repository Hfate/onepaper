package publisher

import (
	"net/http"
	"strings"

	"github.com/Hfate/onepaper/pkg/logger"
	"github.com/silenceper/wechat/v2/officialaccount/message"
)

// RegisterPushHandler 注册公众号「消息与事件」回调（GET 校验、POST 推送）。
// path 如 /api/v1/wechat/serve；Token / EncodingAESKey / AppID 与公众平台「服务器配置」一致。
// 若 token 为空则跳过注册。
func (p *WeChatPublisher) RegisterPushHandler(mux *http.ServeMux, path string) {
	path = strings.TrimSuffix(path, "/")
	if path == "" || strings.TrimSpace(p.cfg.Token) == "" {
		return
	}
	mux.HandleFunc(path, p.handleWeChatPush)
	logger.L.Info("wechat push handler registered", "path", path)
}

func (p *WeChatPublisher) handleWeChatPush(w http.ResponseWriter, r *http.Request) {
	oa := p.oa()
	srv := oa.GetServer(r, w)
	srv.SetMessageHandler(func(msg *message.MixMessage) *message.Reply {
		if msg == nil {
			return nil
		}
		logger.L.Info("wechat push message",
			"msgType", msg.MsgType,
			"event", msg.Event,
			"fromUser", msg.FromUserName,
		)
		// 默认不主动回复正文（返回 nil 时 SDK 在非安全模式下回复 success）
		return nil
	})
	if err := srv.Serve(); err != nil {
		logger.L.Warn("wechat push serve", "err", err)
		http.Error(w, "bad request", http.StatusBadRequest)
	}
}
