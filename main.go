package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"sync"

	log "github.com/Sirupsen/logrus"
	"github.com/codegangsta/cli"
	"github.com/howeyc/fsnotify"
	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

type (
	MachineConfig struct {
		Driver struct {
			IPAddress string `json:"IPAddress,omitempty"`
			SSHPort   int    `json:"SSHPort,omitempty"`
		}
	}
)

var (
	errFlagError      = errors.New("flag error")
	srcPath           string
	destPath          string
	machineName       string
	machineConfigPath string
	machineUser       string
	mutex             = &sync.Mutex{}
	rsftp             *sftp.Client
)

func checkFlags(c *cli.Context) error {
	if c.GlobalString("directory") == "" {
		log.Error("you must specify a directory")
		return errFlagError
	}

	if c.GlobalString("machine") == "" {
		log.Error("you must specify a machine")
		return errFlagError
	}

	if c.GlobalString("destination") == "" {
		log.Error("you must specify a destination path")
		return errFlagError
	}

	if c.GlobalString("user") == "" {
		log.Error("you must specify a user")
		return errFlagError
	}

	if c.GlobalBool("debug") == true {
		log.SetLevel(log.DebugLevel)
	}

	return nil
}

func getMachineConfigDir() string {
	return filepath.Join(machineConfigPath, machineName)
}

func loadConfig() (*MachineConfig, error) {
	c := &MachineConfig{}

	conf := filepath.Join(getMachineConfigDir(), "config.json")
	data, err := os.Open(conf)
	if err != nil {
		return nil, err
	}

	if err := json.NewDecoder(data).Decode(&c); err != nil {
		return nil, err
	}

	return c, nil
}

func watch(c *cli.Context) {
	srcPath = c.GlobalString("directory")
	destPath = c.GlobalString("destination")
	machineName = c.GlobalString("machine")
	machineUser = c.GlobalString("user")
	machineConfigPath = c.GlobalString("machine-path")

	done := make(chan bool)
	errorChan := make(chan error)

	machineConfig, err := loadConfig()
	if err != nil {
		log.Fatal(err)
	}

	keyPath := filepath.Join(getMachineConfigDir(), "id_rsa")

	kc := &keychain{}
	if err := kc.loadPEM(keyPath); err != nil {
		log.Fatal(err)
	}

	sshConfig := &ssh.ClientConfig{
		User: machineUser,
		Auth: []ssh.AuthMethod{
			ssh.PublicKeys(kc),
		},
	}

	sshPort := 22

	if machineConfig.Driver.SSHPort != 0 {
		sshPort = machineConfig.Driver.SSHPort
	}

	ip := "127.0.0.1"
	if machineConfig.Driver.IPAddress != "" {
		ip = machineConfig.Driver.IPAddress
	}

	log.Debugf("connecting host=%s:%d user=%s", ip, sshPort, machineUser)

	sshClient, err := ssh.Dial("tcp", fmt.Sprintf("%s:%d", ip, sshPort), sshConfig)
	if err != nil {
		log.Fatal(err)
	}

	ftp, err := sftp.NewClient(sshClient)
	rsftp = ftp

	log.Debugf("connected to %s", sshClient.RemoteAddr())
	log.Infof("machine sync: src=%s dest=%s machine=%s config-dir=%s", srcPath, destPath, machineName, machineConfigPath)

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		log.Fatal(err)
	}

	go func() {
		for {
			select {
			case ev := <-watcher.Event:
				log.Debug("event:", ev)
				go handleEvent(ev, errorChan)
				//syncMachine(syncCompleteChan, errorChan)
			case err := <-watcher.Error:
				log.Debug("error:", err)
			}
		}
	}()

	go func() {
		for {
			select {
			case err := <-errorChan:
				log.Errorf("error during sync: %s", err)
			}
		}
	}()

	err = watcher.Watch(c.GlobalString("directory"))
	if err != nil {
		log.Fatal(err)
	}

	<-done
	watcher.Close()
}

func handleEvent(evt *fsnotify.FileEvent, errChan chan error) {
	// we cannot use filepath.Join here because if it is a windows client
	// the remote paths will be wrong because the machine is linux
	filePath := fmt.Sprintf("%s/%s", destPath, evt.Name)
	if evt.IsDelete() {
		log.Infof("deleting %s", filePath)
		if err := rsftp.Remove(filePath); err != nil {
			log.Error(err)
			return
		}
	} else {
		// this can probably be more efficient
		log.Infof("updating %s", filePath)
		localFile, err := os.Open(evt.Name)
		if err != nil {
			log.Error(err)
			return
		}
		// don't alert on missing remote files
		_ = rsftp.Remove(filePath)

		remoteFile, err := rsftp.Create(filePath)
		if err != nil {
			log.Error(err)
			return
		}

		// TODO: is not copying binaries correctly
		data, err := ioutil.ReadAll(localFile)
		if err != nil {
			log.Error(err)
			return
		}
		if _, err := remoteFile.Write(data); err != nil {
			log.Error(err)
			return
		}
	}
}

func main() {
	app := cli.NewApp()
	app.Name = "machine-sync"
	app.Usage = "sync files for docker machine"
	app.Action = watch
	app.Before = checkFlags
	app.Flags = []cli.Flag{
		cli.StringFlag{
			Name:  "directory, d",
			Value: "",
			Usage: "path to watch directory",
		},
		cli.StringFlag{
			Name:  "machine, m",
			Value: "",
			Usage: "name of docker machine to sync",
		},
		cli.StringFlag{
			Name:  "machine-path, c",
			Value: filepath.Join(os.Getenv("HOME"), ".docker", "machines"),
			Usage: "path to docker machine config directory",
		},
		cli.StringFlag{
			Name:  "destination, p",
			Value: "",
			Usage: "path on destination machine to sync",
		},
		cli.StringFlag{
			Name:  "user, u",
			Value: "root",
			Usage: "user on machine to use for connection",
		},
		cli.BoolFlag{
			Name:  "debug, D",
			Usage: "enable debug logging",
		},
	}

	app.Run(os.Args)
}
