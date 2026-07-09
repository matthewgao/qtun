package transport

import (
	"crypto/cipher"
	crand "crypto/rand"
	"fmt"
	"io"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog/log"
)

// 数据面（IP 报文）走 QUIC DATAGRAM（不可靠、无序、不重传），而非可靠有序的 stream。
// 原因：把内层 TCP 套在可靠有序的 QUIC stream 里会触发「队头阻塞 + 双重重传」——
// 线路上极小的丢包会被 QUIC 在自己这层重传时阻塞整条流，内层 TCP 随即超时重传，
// 吞吐被打垮（实测走 stream 单流重传爆多、窗口起不来）。改用 datagram 后，隧道表现
// 得像一条真实会丢包的链路，丢包恢复交还给内层 TCP（它本就高效），消除双重重传与 HoL。
// 控制面（ping / 建连 / 关闭检测）仍走 stream（可靠，利于路由与存活判断）。
//
// datagram 自带消息边界，无需 stream 那套 [dataLen] 长度前缀，封包格式简化为：
//   [secure(1)][密文][nonce(12)]   （加密）
//   [secure(1)][明文]              （未配置 --key）
// nonce 仍用每包 crypto/rand，天然唯一，与 stream 上的 ping 共用同一 key 也不会碰撞。

// frameDatagram 把一条已 marshal 的 Envelope 封装为 datagram 负载。每次返回新分配的
// 切片（SendDatagram 会拷贝入发送队列，调用方用完即可丢弃）。aesgcm 为 nil 表示不加密。
// 加密路径下 crypto/rand 读 nonce 失败时返回 nil（调用方丢弃该包，内层 TCP 自行重传）。
func frameDatagram(aesgcm cipher.AEAD, data []byte) []byte {
	if aesgcm == nil {
		out := make([]byte, 1+len(data))
		out[0] = 0
		copy(out[1:], data)
		return out
	}

	nonce := getNonce()
	defer putNonce(nonce)
	if _, err := io.ReadFull(crand.Reader, nonce); err != nil {
		return nil
	}
	ct := aesgcm.Seal(nil, nonce, data, nil)

	out := make([]byte, 1+len(ct)+len(nonce))
	out[0] = 1
	copy(out[1:], ct)
	copy(out[1+len(ct):], nonce)
	return out
}

// decodeDatagram 解开 frameDatagram 的封包，返回明文（已 marshal 的 Envelope 字节）。
// 加密路径返回 aesgcm.Open 新分配的切片；GCM 校验失败（损坏/伪造）直接返回 error，
// 调用方丢弃该包。
func decodeDatagram(aesgcm cipher.AEAD, d []byte) ([]byte, error) {
	if len(d) < 1 {
		return nil, fmt.Errorf("empty datagram")
	}
	if d[0] == 0 {
		return d[1:], nil
	}
	if aesgcm == nil || len(d) < 1+12 {
		return nil, fmt.Errorf("secure datagram but no cipher or too short (len=%d)", len(d))
	}
	nonce := d[len(d)-12:]
	ct := d[1 : len(d)-12]
	return aesgcm.Open(nil, nonce, ct, nil)
}

// lastDatagramWarn 用于节流 SendDatagram 丢包告警（每秒最多一条）。
var lastDatagramWarn int64

// warnDatagramDrop 节流打印 datagram 发送失败。小包失败通常是连接/路由异常；
// 只有报文过大类错误才需要调小 TUN MTU（如 --mtu 1280）。
func warnDatagramDrop(err error, size int) {
	now := time.Now().UnixNano()
	last := atomic.LoadInt64(&lastDatagramWarn)
	if now-last > int64(time.Second) && atomic.CompareAndSwapInt64(&lastDatagramWarn, last, now) {
		log.Warn().Err(err).Int("size", size).
			Msg("Datagram 丢弃数据包（连接/路由异常或报文过大；仅报文过大时调小 --mtu，如 1280）")
	}
}
