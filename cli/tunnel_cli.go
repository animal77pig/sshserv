package main

// from  https://gist.github.com/0187773933/0f1061d6ada5333dbe462ae2bacd7bbd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net"
	"os"
	"os/signal"
	"path"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"golang.org/x/crypto/ssh"
)

// https://github.com/dsnet/sshtunnel

type KeepAliveConfig__ struct {
	// Interval is the amount of time in seconds to wait before the
	// tunnel client will send a keep-alive message to ensure some minimum
	// traffic on the SSH connection.
	Interval uint

	// CountMax is the maximum number of consecutive failed responses to
	// keep-alive messages the client is willing to tolerate before considering
	// the SSH connection as dead.
	CountMax uint
}

type bindConf struct {
	mode     byte // '>' for forward, '<' for reverse
	bindAddr string
	dialAddr string
}

func (t *bindConf) String() string {
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

type STun struct {
	auth          []ssh.AuthMethod
	hostKeys      ssh.HostKeyCallback
	user          string
	hostAddr      string
	retryInterval time.Duration
	keepAlive     KeepAliveConfig__

	binds []bindConf // one ssh connection bind many tunnel

}

func (t STun) String() string {
	// var tt []string
	// for _, x := range t.binds {
	// 	tt = append(tt, x.String())
	// }

	// return fmt.Sprintf("%s@%s | %s", t.user, t.hostAddr, strings.Join(tt, ";  "))
	return fmt.Sprintf("%s@%s", t.user, t.hostAddr)
}

func (t STun) bindTunnel(ctx context.Context, wg *sync.WaitGroup) {
	defer wg.Done()

	for {
		var once sync.Once // Only print errors once per session
		func() {
			// Connect to the server host via SSH.
			cl, err := ssh.Dial("tcp", t.hostAddr, &ssh.ClientConfig{
				User:            t.user,
				Auth:            t.auth,
				HostKeyCallback: t.hostKeys,
				Timeout:         5 * time.Second,
			})
			if err != nil {
				once.Do(func() { fmt.Printf("(%v) SSH dial error: %v\n", t, err) })
				return
			}
			wg.Add(1)

			defer cl.Close()

			// Attempt to bind to the inbound socket.
			for _, _x := range t.binds {
				//wg.Add(1)
				go func(x bindConf) {
					log.Printf("====x: %s\n", x.String())
					var ln net.Listener
					switch x.mode {
					case '>':
						ln, err = net.Listen("tcp", x.bindAddr)
					case '<':
						ln, err = cl.Listen("tcp", x.bindAddr)
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
						go x.dialTunnel(bindCtx, wg, cl, cn1)
					}

				}(_x)

			}

			log.Printf("keep alive before\n")
			t.keepAliveMonitor(&once, wg, cl)
			log.Printf("keep alive after\n")
		}()

		select {
		case <-ctx.Done():
			return
		case <-time.After(t.retryInterval):
			fmt.Printf("(%v) retrying...\n", t)
		}
	}
}

func (x bindConf) dialTunnel(ctx context.Context, wg *sync.WaitGroup, client *ssh.Client, cn1 net.Conn) {
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
	switch x.mode {
	case '>':
		cn2, err = client.Dial("tcp", x.dialAddr)
	case '<':
		cn2, err = net.Dial("tcp", x.dialAddr)
	}
	if err != nil {
		fmt.Printf("(%v) dial error: %v\n", x, err)
		return
	}

	go func() {
		<-connCtx.Done()
		cn2.Close()
	}()

	fmt.Printf("(%v) connection established\n", x.dialAddr)
	defer fmt.Printf("(%v) connection closed\n", x.dialAddr)

	// Copy bytes from one connection to the other until one side closes.
	var once sync.Once
	var wg2 sync.WaitGroup
	wg2.Add(2)
	go func() {
		defer wg2.Done()
		defer cancel()
		if _, err := io.Copy(cn1, cn2); err != nil {
			once.Do(func() { fmt.Printf("(%v) connection error: %v", x, err) })
		}
		once.Do(func() {}) // Suppress future errors
	}()
	go func() {
		defer wg2.Done()
		defer cancel()
		if _, err := io.Copy(cn2, cn1); err != nil {
			once.Do(func() { fmt.Printf("(%v) connection error: %v", x, err) })
		}
		once.Do(func() {}) // Suppress future errors
	}()
	wg2.Wait()
}

// keepAliveMonitor periodically sends messages to invoke a response.
// If the server does not respond after some period of time,
// assume that the underlying net.Conn abruptly died.
func (t STun) keepAliveMonitor(once *sync.Once, wg *sync.WaitGroup, client *ssh.Client) {
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
				once.Do(func() { fmt.Printf("(%v) SSH error: %v", t, err) })
			}
			return
		case <-ticker.C:
			if n := atomic.AddInt32(&aliveCount, 1); n > int32(t.keepAlive.CountMax) {
				once.Do(func() { fmt.Printf("(%v) SSH keep-alive termination", t) })
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

	Tunnels []struct {
		// The syntax of a forward tunnel is:
		//	"bind_address:port -> dial_address:port"
		// The syntax of a reverse tunnel is:
		//	"dial_address:port <- bind_address:port"
		Tunnel string   `json:"tunnel"`
		Binds  []string `json:"binds"`

		// Server is a remote SSH host. It has the following syntax:
		//	"user@host:port"
		//
		// If the user is missing, then it defaults to the current process user.
		// If the port is missing, then it defaults to 22.
		Server   string `json:"server"`
		RetrySec int    `json:"retry_sec`
	} `json: tunnels`
}

/*

:: on office desktop
{
	"keyfile":"d:/devtool/bin/id_rsa",
	"tunnels":[
		{
		 "binds": [
				"localhost:2022 <- localhost:2022",
				"127.0.0.1:5900 <- localhost:5900",
				"0.0.0.0:21080 -> localhost:21080"
				],
		 "server":"app@121.43.230.103:22", "retry_sec":300}
	]
}

*/
func loadConf(conf_file string) (tunns []STun, closer func() error) {
	configJson, err := ioutil.ReadFile(conf_file)
	if err != nil {
		log.Printf("ReadFile Error:%v\n", err)
		return nil, closer
	}
	var conf TunnelConf
	err = json.Unmarshal(configJson, &conf)
	if err != nil {
		log.Printf("json decode failed: %v\n", err)
		return nil, closer
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

	for _, t := range conf.Tunnels {

		var tunn STun
		tunn.auth = auth
		tunn.hostKeys = func(hostname string, remote net.Addr, key ssh.PublicKey) error {
			return nil
		}

		// user@172.18.231.76:7122
		re := regexp.MustCompile("^([^@]+)@([^:]+):([0-9]+)")
		reout := re.FindStringSubmatch(t.Server)
		if len(reout) > 0 {
			tunn.user = reout[1]
			tunn.hostAddr = net.JoinHostPort(reout[2], reout[3])
		} else {
			log.Printf("--- %v, %d [%s]\n", reout, len(reout), t.Server)
			continue
		}
		tunn.retryInterval = time.Duration(t.RetrySec) * time.Second
		tunn.keepAlive.Interval = 10
		tunn.keepAlive.CountMax = 99999999

		for _, bindstr := range t.Binds { // many tunnel on on SSH connection
			tt := strings.Split(bindstr, " ")
			if len(tt) != 3 {
				log.Printf("invalid Tunnel config: %s\n", t.Tunnel)
				continue
			}
			if tt[1] == "->" { // forward tunnel
				tunn.binds = append(tunn.binds, bindConf{mode: '>', bindAddr: tt[0], dialAddr: tt[2]})
			} else if tt[1] == "<-" { // reverse tunnel
				tunn.binds = append(tunn.binds, bindConf{mode: '<', bindAddr: tt[0], dialAddr: tt[2]})
			}
		}
		tunns = append(tunns, tunn)
	}
	return tunns, closer
}

func SSHTunnel(conf_file string) {
	tunns, closer := loadConf(conf_file)
	defer closer()

	// Setup signal handler to initiate shutdown.
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		sigc := make(chan os.Signal, 1)
		signal.Notify(sigc, syscall.SIGINT, syscall.SIGTERM)
		fmt.Printf("received %v - initiating shutdown\n", <-sigc)
		cancel()
	}()

	// Start a bridge for each tunnel.
	var wg sync.WaitGroup
	fmt.Printf("%s starting\n", path.Base(os.Args[0]))
	defer fmt.Printf("%s shutdown\n", path.Base(os.Args[0]))
	for _, t := range tunns {
		wg.Add(1)
		go t.bindTunnel(ctx, &wg)
	}
	wg.Wait()
}

func main() {
	SSHTunnel(os.Args[1])
}
