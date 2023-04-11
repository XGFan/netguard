package main

import (
	"flag"
	"github.com/XGFan/go-utils"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/lightsail"
	"gopkg.in/yaml.v3"
	"log"
	"time"
)

const (
	running  = "running"
	stopping = "stopping"
	stopped  = "stopped"
)

type Config struct {
	Instance   string     `yaml:"instance,omitempty"`
	Region     string     `yaml:"region,omitempty"`
	Credential Credential `yaml:"credential"`
}
type Credential struct {
	Id     string `yaml:"id,omitempty"`
	Secret string `yaml:"secret,omitempty"`
}

type LightNode struct {
	instanceName string
	svc          *lightsail.Lightsail
}

func (ln LightNode) getInstance() *lightsail.Instance {
	instance, err := ln.svc.GetInstance(&lightsail.GetInstanceInput{
		InstanceName: aws.String(ln.instanceName),
	})
	if err != nil {
		log.Panic(err)
	}
	return instance.Instance
}

func (ln LightNode) Start(wait bool) {
	log.Println("checking the status")
	stableStatus := ln.waitToStable()
	if stableStatus == running {
		log.Printf("instance[%s] is already running", ln.instanceName)
		instance := ln.getInstance()
		log.Printf("current ip: %s", *instance.PublicIpAddress)
	} else if stableStatus == stopped {
		log.Println("try to start instance")
		_, err := ln.svc.StartInstance(&lightsail.StartInstanceInput{
			InstanceName: aws.String(ln.instanceName),
		})
		if err != nil {
			log.Printf("start instance[%s] fail: %s", ln.instanceName, err)
		}
		if wait {
			ln.wait(running)
			instance := ln.getInstance()
			log.Printf("current ip: %s", *instance.PublicIpAddress)
		}
	} else {
		log.Printf("can't start instance in current state")
	}
}

func (ln LightNode) Stop(wait bool) {
	log.Println("checking the status")
	stableStatus := ln.waitToStable()
	if stableStatus == stopped {
		log.Printf("instance[%s] is already stopped", ln.instanceName)
	} else if stableStatus == running {
		log.Println("try to stop instance")
		_, err := ln.svc.StopInstance(&lightsail.StopInstanceInput{
			InstanceName: aws.String(ln.instanceName),
		})
		if err != nil {
			log.Printf("stop instance[%s] fail: %s", ln.instanceName, err)
		}
		if wait {
			ln.wait(stopped)
		}
	} else {
		log.Printf("can't stop instance in current state")
	}
}

func (ln LightNode) wait(status string) {
	log.Printf("wait to %s", status)
	for i := 0; i < 30; i++ {
		instance := ln.getInstance()
		if *instance.State.Name == status {
			log.Printf("instance: %s", status)
			return
		} else {
			log.Printf("instance is in status: %v", *instance.State.Name)
			time.Sleep(10 * time.Second)
		}
	}
	log.Printf("wait timeout, exit")
}

func (ln LightNode) waitToStable() string {
	for i := 0; i < 30; i++ {
		instance := ln.getInstance()
		if *instance.State.Name == running || *instance.State.Name == stopped {
			return *instance.State.Name
		} else {
			log.Printf("instance is in status: %v, waiting", *instance.State.Name)
			time.Sleep(10 * time.Second)
		}
	}
	log.Printf("wait timeout, exit")
	return ""
}

func main() {
	file, err := utils.LocateAndRead("config.yaml")
	if err != nil {
		log.Panic(err)
	}
	config := Config{}
	err = yaml.Unmarshal(file, &config)
	if err != nil {
		log.Panic(err)
	}
	flag.Parse()
	mySession := session.Must(session.NewSession())
	// Create a Lightsail client with additional configuration
	svc := lightsail.New(mySession, aws.NewConfig().
		//WithLogLevel(aws.LogDebug).
		WithRegion(config.Region).
		WithCredentials(credentials.NewStaticCredentials(config.Credential.Id, config.Credential.Secret, "")))
	arg := flag.Arg(0)
	ln := LightNode{
		config.Instance,
		svc,
	}
	switch arg {
	case "ip":
		instance := ln.getInstance()
		log.Printf("ip: %s", *instance.PublicIpAddress)
		return
	case "stop":
		ln.Stop(true)
		return
	case "start":
		ln.Start(true)
		return
	case "restart":
		ln.Stop(true)
		ln.Start(true)
		return
	}
}
