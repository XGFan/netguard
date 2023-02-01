package main

import (
	"context"
	"flag"
	"fmt"
	"gopkg.in/yaml.v3"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"time"
)

type Checker struct {
	Name      string        `yaml:"name"`
	Targets   []Target      `yaml:"targets"`
	Proxy     string        `yaml:"proxy"`
	Threshold int           `yaml:"threshold"`
	PostUp    string        `yaml:"postUp"`
	PostDown  string        `yaml:"postDown"`
	Timeout   time.Duration `yaml:"timeout"`
	//----------
	httpClient http.Client
	status     int
	failCount  int
}

type Target struct {
	IP   string `yaml:"ip"`
	Host string `yaml:"host"`
}

func main() {
	file := flag.String("c", "config.yaml", "config location")
	flag.Parse()
	log.Println("Network Guard")
	open, err := os.Open(*file)
	if err != nil {
		log.Fatalf("read config error: %s", err)
	}
	checkers := new([]Checker)
	err = yaml.NewDecoder(open).Decode(checkers)
	if err != nil {
		log.Fatalf("parse config error: %s", err)
	}
	ctx := context.Background()
	for _, c := range *checkers {
		go func(checker Checker) {
			checker.Check(ctx)
		}(c)
	}
	select {}
}

const (
	UP = iota
	DOWN
)

func (c *Checker) Check(ctx context.Context) {
	log.Printf("%+v", c)
	var proxy = http.ProxyFromEnvironment
	if strings.TrimSpace(c.Proxy) != "" {
		parse, err := url.Parse(c.Proxy)
		if err == nil {
			proxy = http.ProxyURL(parse)
		}
	}
	c.httpClient = http.Client{
		Transport: &http.Transport{Proxy: proxy},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
		Timeout: 10 * time.Second,
	}
	for {
		select {
		case <-ctx.Done():
		default:
			c.httpClient.CloseIdleConnections()
			ret := c.HttpCheck(ctx)
			log.Printf("%s check result: %v", c.Name, ret)
			switch c.status {
			case UP:
				if ret.Status {
					if c.failCount != 0 {
						log.Printf("%s recover", c.Name)
					}
					c.failCount = 0
				} else {
					c.failCount++
					if c.failCount >= c.Threshold {
						log.Printf("%s from UP to DOWN", c.Name)
						c.status = DOWN
						c.failCount = c.Threshold
						if c.PostDown != "" {
							RunExternalCmd(c.PostDown)
							log.Printf("%s PostDown executed", c.Name)
						}
					} else {
						log.Printf("%s jitter", c.Name)
					}
				}
			case DOWN:
				if ret.Status {
					c.failCount -= 1
					if c.failCount <= 0 {
						log.Printf("%s from DOWN to UP", c.Name)
						c.status = UP
						c.failCount = 0
						if c.PostUp != "" {
							RunExternalCmd(c.PostDown)
							log.Printf("%s PostUp executed", c.Name)
						}
					}
				} else {
					c.failCount = c.Threshold
				}
			}
			time.Sleep(5 * time.Second)
		}
	}

}

func (c *Checker) HttpCheck(pctx context.Context) CheckResult {
	result := make(chan interface{})
	ctx, cancelFunc := context.WithTimeout(pctx, c.Timeout)
	defer cancelFunc()
	for _, target := range c.Targets {
		go func(t Target) {
			log.Printf("try to check %s", t)
			status := HttpCheck(c.httpClient, ctx, t)
			select {
			case <-ctx.Done():
				return
			case result <- status:
				return
			}
		}(target)
	}
	timer := time.NewTimer(c.Timeout)
	for {
		select {
		case ret := <-result:
			if r, ok := ret.(CheckResult); ok {
				if r.Status {
					return r
				}
			}
		case <-timer.C:
			log.Printf("%s all targets timeout", c.Name)
			return CheckResult{Status: false}
		}
	}
}

type CheckResult struct {
	Status  bool
	Message string
}

var StatusOk = CheckResult{true, ""}

func HttpCheck(httpClient http.Client, ctx context.Context, target Target) CheckResult {
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, fmt.Sprintf("http://%s", target.IP), nil)
	if err != nil {
		return CheckResult{false, fmt.Sprintf("create request fail: %s", err.Error())}
	}
	req.Header.Set("Host", target.Host)
	resp, err := httpClient.Do(req)
	if err != nil {
		return CheckResult{false, fmt.Sprintf("send request fail: %s", err.Error())}
	}
	defer resp.Body.Close()
	return StatusOk
}

func TcpPing(addr string) error {
	tcpAddr, err := net.ResolveTCPAddr("", addr)
	if err != nil {
		return err
	}
	dialer, err := net.DialTCP("tcp", nil, tcpAddr)
	_, err = dialer.Write([]byte{})
	return err
}

func RunExternalCmd(cmd string) {
	split := strings.Split(cmd, " ")
	output, err := exec.Command(split[0], split[1:]...).Output()
	if err != nil {
		log.Println(err)
	}
	log.Println(string(output))
}
