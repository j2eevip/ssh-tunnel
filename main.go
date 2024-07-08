package main

import (
	"errors"
	"io"
	"log"
	"net"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"golang.org/x/crypto/ssh"
)

type TunnelConfig struct {
	LocalAddr  string
	ProxyAddr  string
	TunnelAddr string
	TunnelUser string
	TunnelPwd  string
	StopCh     chan struct{}
}

func main() {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT, syscall.SIGKILL)

	tc := &TunnelConfig{
		LocalAddr:  os.Getenv("LOCAL_ADDR"),
		ProxyAddr:  os.Getenv("PROXY_ADDR"),
		TunnelAddr: os.Getenv("TUNNEL_ADDR"),
		TunnelUser: os.Getenv("TUNNEL_USER"),
		TunnelPwd:  os.Getenv("TUNNEL_PWD"),
		StopCh:     make(chan struct{}),
	}

	go func() {
		sig := <-sigCh
		log.Printf("exit. sig: %s", sig.String())
		log.Println("send stop.")
		tc.StopCh <- struct{}{}
	}()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		if err := tc.Run(); err != nil {
			log.Fatalf("Error: run forward error %s", err)
		}
		wg.Done()
	}()
	wg.Wait()
}

func (tc TunnelConfig) Run() error {
	localServer, err := net.Listen("tcp", tc.LocalAddr)
	if err != nil {
		return err
	}
	log.Printf("initialize local listener success :%s", tc.LocalAddr)

	config := &ssh.ClientConfig{
		User: tc.TunnelUser,
		Auth: []ssh.AuthMethod{
			ssh.Password(tc.TunnelPwd), // SSH密码
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
	}
	tunnelConn, err := ssh.Dial("tcp", tc.TunnelAddr, config)
	if err != nil {
		return err
	}
	log.Printf("initialize local tunnel success :%s", tc.TunnelAddr)

	proxyConn, err := tunnelConn.Dial("tcp", tc.ProxyAddr)
	if err != nil {
		return err
	}
	log.Printf("initialize proxy server success: %s", tc.ProxyAddr)

	go func() {
		<-tc.StopCh
		_ = localServer.Close()
		_ = tunnelConn.Close()
		_ = proxyConn.Close()
		log.Printf("sinal tunnel exit.")
	}()

	for {
		localConn, err := localServer.Accept()
		if err != nil || errors.Is(err, net.ErrClosed) {
			return err
		}

		go func() {
			size, err := io.Copy(localConn, proxyConn)
			if err != nil {
				log.Printf("Error: Failed to copy data: %v", err)
			} else {
				log.Printf("local to target %s ---> %s ---> %s , size: %d", tc.LocalAddr, tc.TunnelAddr, tc.ProxyAddr, size)
			}
		}()

		go func() {
			size, err := io.Copy(proxyConn, localConn)
			if err != nil {
				log.Printf("ERROR: Failed to copy data: %v", err)
			} else {
				log.Printf("target to local %s ---> %s ---> %s, size: %d", tc.ProxyAddr, tc.TunnelAddr, tc.LocalAddr, size)
			}
		}()
	}
	return nil
}
