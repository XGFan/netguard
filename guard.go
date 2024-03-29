package netguard

import (
	"context"
	"errors"
	"fmt"
	"github.com/XGFan/go-utils"
	"log"
	"net"
	"net/http"
	_ "net/http/pprof"
	"net/url"
	"os/exec"
	"strings"
	"time"
)

type Checker struct {
	Name      string
	Targets   []Target
	Proxy     string
	Threshold int
	PostUp    func()
	PostDown  func()
	Timeout   time.Duration
	//----------
	httpClient http.Client
	status     int
	failCount  int
}

type Target struct {
	IP   string `yaml:"ip"`
	Host string `yaml:"host"`
}

const (
	DOWN = iota
	UP
)

func (c *Checker) Status() int {
	return c.status
}

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
		Timeout: c.Timeout,
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
						if c.PostDown != nil {
							c.PostDown()
						}
					} else {
						log.Printf("%s jitter", c.Name)
					}
				}
			case DOWN:
				if ret.Status {
					log.Printf("%s from DOWN to UP", c.Name)
					c.status = UP
					c.failCount = 0
					if c.PostUp != nil {
						c.PostUp()
					}
				}
			}
			time.Sleep(5 * time.Second)
		}
	}
}

func (c *Checker) HttpCheck(pctx context.Context) CheckResult {
	ret, err := utils.RaceResultWithError[Target, CheckResult](c.Targets, func(t Target) (CheckResult, error) {
		log.Printf("try to check %s", t)
		ret := HttpCheck(c.httpClient, pctx, t)
		if ret.Status {
			return ret, nil
		} else {
			return CheckResult{}, errors.New(ret.Message)
		}
	}, c.Timeout)
	if err == nil {
		return ret
	} else {
		return CheckResult{Status: false}
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

type CheckerConf struct {
	Name      string        `yaml:"name"`
	Targets   []Target      `yaml:"targets"`
	Proxy     string        `yaml:"proxy"`
	Threshold int           `yaml:"threshold"`
	PostUp    string        `yaml:"postUp"`
	PostDown  string        `yaml:"postDown"`
	Timeout   time.Duration `yaml:"timeout"`
}

func AssembleChecker(conf CheckerConf) Checker {
	return Checker{
		Name:      conf.Name,
		Targets:   conf.Targets,
		Proxy:     conf.Proxy,
		Threshold: conf.Threshold,
		PostUp: func() {
			RunExternalCmd(conf.PostUp)
			log.Printf("%s PostUp executed", conf.Name)
		},
		PostDown: func() {
			RunExternalCmd(conf.PostDown)
			log.Printf("%s PostDown executed", conf.Name)
		},
		Timeout: conf.Timeout,
	}
}

func RunExternalCmd(cmd string) {
	split := strings.Split(cmd, " ")
	output, err := exec.Command(split[0], split[1:]...).Output()
	if err != nil {
		log.Println(err)
	}
	log.Println(string(output))
}
