package cmd

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/lulugyf/sshserv/hh"

	"github.com/gorilla/websocket"
	"golang.org/x/crypto/ssh"
)

type KeepAliveConfig struct {
	Interval uint
	CountMax uint
}

/*
封装 websocket.Conn 类型以满足 net.Conn or io.Reader or io.Writer
*/
type WS struct {
	c      *websocket.Conn
	remain []byte
}

func (ws *WS) Close() error {
	return ws.c.Close()
}

func (ws *WS) LocalAddr() net.Addr {
	return ws.c.LocalAddr()
}

func (ws *WS) RemoteAddr() net.Addr {
	return ws.c.RemoteAddr()
}

func (ws *WS) SetDeadline(t time.Time) error {
	return ws.c.SetReadDeadline(t)
}

func (ws *WS) SetReadDeadline(t time.Time) error {
	return ws.c.SetReadDeadline(t)
}

func (ws *WS) SetWriteDeadline(t time.Time) error {
	return ws.c.SetWriteDeadline(t)
}

func (ws *WS) Read(p []byte) (int, error) {
	var message []byte
	var err error

	if ws.remain != nil {
		message = ws.remain
	} else {
		_, message, err = ws.c.ReadMessage()
		if err != nil {
			return -1, err
		}
	}

	if len(message) <= len(p) {
		ws.remain = nil
		return copy(p, message), nil
	} else {
		ws.remain = message[len(p):]
		return copy(p, message), nil
	}
}
func (ws *WS) Write(p []byte) (int, error) {
	err := ws.c.WriteMessage(websocket.BinaryMessage, p)
	if err != nil {
		return -1, err
	}
	return len(p), nil
}

/*
*
一次 request-response 请求
*/
func (ws *WS) Req(msg string) (string, error) {
	err := ws.c.WriteMessage(websocket.TextMessage, []byte(msg))
	if err != nil {
		return "", err
	}
	_, rdata, err := ws.c.ReadMessage()
	if err != nil {
		return "", err
	}
	return string(rdata), nil
}

type _port struct {
	mode     byte // '>' for forward, '<' for reverse
	bindAddr string
	dialAddr string
}
type tunnel struct {
	auth          []ssh.AuthMethod
	hostKeys      ssh.HostKeyCallback
	user          string
	hostAddr      string
	retryInterval time.Duration
	keepAlive     KeepAliveConfig

	//mode     byte // '>' for forward, '<' for reverse
	//bindAddr string
	//dialAddr string

	ports []_port // 单个连接多个端口映射， 只有单个端口的时候使用上面的配置， 多于一个其它的则在此配置
}

func (t _port) bind(ctx context.Context, wg *sync.WaitGroup, cl *ssh.Client) {
	var err error
	var once sync.Once

	// Attempt to bind to the inbound socket.
	var ln net.Listener

	defer wg.Done()

	switch t.mode {
	case '>':
		ln, err = net.Listen("tcp", t.bindAddr)
	case '<':
		ln, err = cl.Listen("tcp", t.bindAddr)
	}
	if err != nil {
		once.Do(func() { fmt.Printf("(%v) bind error: %v\n", t, err) })
		return
	}

	// The socket is binded. Make sure we close it eventually.
	bindCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() {
		cl.Wait()
		cancel()
	}()
	go func() {
		<-bindCtx.Done()
		once.Do(func() {}) // Suppress future errors
		ln.Close()
	}()

	fmt.Printf("(%v) binded tunnel\n", t)
	defer fmt.Printf("(%v) collapsed tunnel\n", t)

	// Accept all incoming connections.
	for {
		cn1, err := ln.Accept()
		if err != nil {
			once.Do(func() { fmt.Printf("(%v) accept error: %v\n", t, err) })
			return
		}
		wg.Add(1)
		go t.dial(bindCtx, wg, cl, cn1)
	}
}

func (t _port) dial(ctx context.Context, wg *sync.WaitGroup, client *ssh.Client, cn1 net.Conn) {
	defer wg.Done()

	// The inbound connection is established. Make sure we close it eventually.
	connCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() {
		<-connCtx.Done()
		cn1.Close()
	}()

	// Establish the outbound connection.
	var cn2 net.Conn
	var err error
	switch t.mode {
	case '>':
		cn2, err = client.Dial("tcp", t.dialAddr)
	case '<':
		cn2, err = net.Dial("tcp", t.dialAddr)
	}
	if err != nil {
		hh.Error("(%v) dial error: %v", t, err)
		return
	}

	go func() {
		<-connCtx.Done()
		cn2.Close()
	}()

	hh.Info("(%v) connection established", t)
	defer hh.Warn("(%v) connection closed", t)

	// Copy bytes from one connection to the other until one side closes.
	var once sync.Once
	var wg2 sync.WaitGroup
	wg2.Add(2)
	go func() {
		defer wg2.Done()
		defer cancel()
		if _, err := io.Copy(cn1, cn2); err != nil {
			once.Do(func() { hh.Error("(%v) connection error: %v", t, err) })
		}
		once.Do(func() {}) // Suppress future errors
	}()
	go func() {
		defer wg2.Done()
		defer cancel()
		if _, err := io.Copy(cn2, cn1); err != nil {
			once.Do(func() { hh.Error("(%v) connection error: %v", t, err) })
		}
		once.Do(func() {}) // Suppress future errors
	}()
	wg2.Wait()
}

func (t _port) String() string {
	var left, right string
	mode := "<?>"
	switch t.mode {
	case '>':
		left, mode, right = t.bindAddr, "->", t.dialAddr
	case '<':
		left, mode, right = t.dialAddr, "<-", t.bindAddr
	}
	return fmt.Sprintf("%s %s %s", left, mode, right)
}

//func (t tunnel) String() string {
//	var left, right string
//	mode := "<?>"
//	switch t.mode {
//	case '>':
//		left, mode, right = t.bindAddr, "->", t.dialAddr
//	case '<':
//		left, mode, right = t.dialAddr, "<-", t.bindAddr
//	}
//	return fmt.Sprintf("%s@%s | %s %s %s", t.user, t.hostAddr, left, mode, right)
//}

func (t tunnel) bindTunnel(ctx context.Context, wg *sync.WaitGroup) {
	defer wg.Done()

	for {
		var once sync.Once // Only print errors once per session
		func() {
			// Connect to the server host via SSH.
			var cl *ssh.Client = nil
			var err error

			if strings.HasPrefix(t.hostAddr, "ws://") || strings.HasPrefix(t.hostAddr, "wss://") {

				// 通过 ws 连接产生 ssh.Client
				var InsecureWSDialer = &websocket.Dialer{
					Proxy:            http.ProxyFromEnvironment,
					HandshakeTimeout: 45 * time.Second,
					TLSClientConfig: &tls.Config{
						InsecureSkipVerify: true,
					},
				}
				conn, _, err := InsecureWSDialer.Dial(t.hostAddr, nil)
				if err != nil {
					hh.Error("websocket dial failed %s, %v", t.hostAddr, err)
					return
				}
				config := &ssh.ClientConfig{
					User:            t.user,
					Auth:            t.auth,
					HostKeyCallback: t.hostKeys,
					Timeout:         5 * time.Second,
				}
				ws := &WS{c: conn}
				c, chans, reqs, err := ssh.NewClientConn(ws, "127.0.0.1:22", config)
				if err != nil {
					hh.Error("ssh.NewClientConn failed: %v\n", err)
					return
				}
				cl = ssh.NewClient(c, chans, reqs)
			} else {
				cl, err = ssh.Dial("tcp", t.hostAddr, &ssh.ClientConfig{
					User:            t.user,
					Auth:            t.auth,
					HostKeyCallback: t.hostKeys,
					Timeout:         5 * time.Second,
				})
				if err != nil {
					once.Do(func() { hh.Error("(%v) SSH dial error: %v\n", t, err) })
					return
				}
			}
			wg.Add(1)
			go t.keepAliveMonitor(&once, wg, cl)
			defer cl.Close()

			// 多端口映射
			for _, port := range t.ports[1:] {
				go port.bind(ctx, wg, cl)
			}
			t.ports[0].bind(ctx, wg, cl)
		}()

		select {
		case <-ctx.Done():
			return
		case <-time.After(t.retryInterval): //time.Second * 30):
			hh.Warn("(%v) retrying interval: %v...", t, t.retryInterval.Seconds())
		}
	}
}

// keepAliveMonitor periodically sends messages to invoke a response.
// If the server does not respond after some period of time,
// assume that the underlying net.Conn abruptly died.
func (t tunnel) keepAliveMonitor(once *sync.Once, wg *sync.WaitGroup, client *ssh.Client) {
	defer wg.Done()
	if t.keepAlive.Interval == 0 || t.keepAlive.CountMax == 0 {
		return
	}

	// Detect when the SSH connection is closed.
	wait := make(chan error, 1)
	wg.Add(1)
	go func() {
		defer wg.Done()
		wait <- client.Wait()
	}()

	// Repeatedly check if the remote server is still alive.
	var aliveCount int32
	ticker := time.NewTicker(time.Duration(t.keepAlive.Interval) * time.Second)
	defer ticker.Stop()
	for {
		select {
		case err := <-wait:
			if err != nil && err != io.EOF {
				once.Do(func() { hh.Error("(%v) SSH error: %v", t, err) })
			}
			return
		case <-ticker.C:
			if n := atomic.AddInt32(&aliveCount, 1); n > int32(t.keepAlive.CountMax) {
				once.Do(func() { hh.Error("(%v) SSH keep-alive termination", t) })
				client.Close()
				return
			}
		}

		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _, err := client.SendRequest("keepalive@openssh.com", true, nil)
			if err == nil {
				atomic.StoreInt32(&aliveCount, 0)
			}
		}()
	}
}

type TunnelConf struct {
	Pass_OR_Keyfile string `json:"keyfile"`
	Server          string `json:"server"`
	RetrySec        int    `json:"retry_sec"`
	User            string `json:"user"`

	Ports []string `json:"ports"`
}

/*
		{
		 "server_":"121.43.230.103:22",
		 "server":"wss://121.43.230.103:443/yt/ws",
		 "user": "_base_",
		 "retry_sec":300,
		 "keyfile":"d:/devtool/bin/id_rsa",
		 "ports": [
			"2022;<;2022", "5900;<;5900", "21080;>;21080"
	     ]
	}
*/
func loadConf(conf_file string) (*tunnel, error) {
	configJson, err := ioutil.ReadFile(conf_file)
	if err != nil {
		hh.Error("ReadFile Error:%v", err)
		return nil, errors.New("read file failed")
	}
	var conf TunnelConf
	err = json.Unmarshal(configJson, &conf)
	if err != nil {
		hh.Error("json decode failed: %v", err)
		return nil, errors.New("json decode failed")
	}
	var auth []ssh.AuthMethod
	_, err = os.Stat(conf.Pass_OR_Keyfile)
	if err == nil {
		pemBytes, err := ioutil.ReadFile(conf.Pass_OR_Keyfile)
		signer, err := ssh.ParsePrivateKey(pemBytes)
		if err == nil {
			auth = append(auth, ssh.PublicKeys(signer))
		}
	} else {
		auth = append(auth, ssh.Password(conf.Pass_OR_Keyfile))
	}

	var tunn tunnel
	tunn.auth = auth
	tunn.hostKeys = func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		return nil
	}
	tunn.retryInterval = time.Duration(conf.RetrySec) * time.Second
	tunn.user = conf.User
	tunn.hostAddr = conf.Server

	for _, p := range conf.Ports {
		tt := strings.Split(p, ";")
		if len(tt) != 3 {
			hh.Error("invalid Tunnel config: %s", conf.Ports)
			continue
		}
		pt := _port{mode: tt[1][0]}
		if pt.mode == '<' {
			pt.bindAddr = fmt.Sprintf("localhost:%s", tt[2])
			pt.dialAddr = fmt.Sprintf("localhost:%s", tt[0])
		} else if pt.mode == '>' {
			pt.bindAddr = fmt.Sprintf("localhost:%s", tt[0])
			pt.dialAddr = fmt.Sprintf("localhost:%s", tt[2])
		}
		tunn.ports = append(tunn.ports, pt)
	}

	return &tunn, nil
}

/*
*
单 ssh 连接多端口映射
ports 格式：

	“{lport}.>.{rport}”  (local)
	"{lport}.<.{rport}"  (remote)
*/
func WSTunnel(conf_file string) {
	tunn, err := loadConf(conf_file)

	if err != nil {
		hh.Error("conf load failed")
		return
	}

	// Setup signal handler to initiate shutdown.
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		sigc := make(chan os.Signal, 1)
		signal.Notify(sigc, syscall.SIGINT, syscall.SIGTERM)
		hh.Warn("received %v - initiating shutdown", <-sigc)
		cancel()
	}()

	var wg sync.WaitGroup
	hh.Info("WSTunnel starting")
	defer hh.Info("WSTunnel shutdown")

	wg.Add(1)
	go tunn.bindTunnel(ctx, &wg)

	wg.Wait()
}
