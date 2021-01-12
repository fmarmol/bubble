package main

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/docker/docker/api/types"
	ac "github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	"github.com/sirupsen/logrus"
	flag "github.com/spf13/pflag"
)

func copyContainer(client *client.Client, container types.Container, n uint64) error {
	infos, err := client.ContainerInspect(context.Background(), container.ID)
	if err != nil {
		return fmt.Errorf("could not inspect container id %s: %w", container.ID, err)
	}
	containerConfig := infos.Config
	hostConfig := infos.ContainerJSONBase.HostConfig
	networkConfig := &network.NetworkingConfig{
		EndpointsConfig: container.NetworkSettings.Networks,
	}
	for i := uint64(0); i < n; i++ {
		createdBody, err := client.ContainerCreate(
			context.Background(),
			containerConfig,
			hostConfig,
			networkConfig,
			nil,
			"",
		)
		if err != nil {
			return fmt.Errorf("could not create container: %w", err)
		}
		for _, warning := range createdBody.Warnings {
			logrus.Warn(warning)
		}
		logrus.WithField("container", createdBody.ID).Info("create container")
		if err := client.ContainerStart(context.Background(), createdBody.ID, types.ContainerStartOptions{}); err != nil {
			return fmt.Errorf("could not start container id %s: %w", createdBody.ID, err)
		}
		logrus.WithField("container", createdBody.ID).Info("start container")
	}
	return nil
}

func deleteContainer(client *client.Client, candidates []types.Container, n uint64) error {
	if int(n) > len(candidates) {
		return fmt.Errorf("can not delete %v containers when exists only %v", n, len(candidates))
	}
	for i := uint64(0); i < n; i++ {
		container := candidates[rand.Intn(len(candidates))]
		if err := client.ContainerStop(context.Background(), container.ID, nil); err != nil {
			return fmt.Errorf("could not stop container id: %s: %w", container.ID, err)
		}
		logrus.WithField("container", container.ID).Info("stop container")
		readyCh, _ := client.ContainerWait(context.Background(), container.ID, ac.WaitConditionNotRunning)
		<-readyCh
		if err := client.ContainerRemove(context.Background(), container.ID, types.ContainerRemoveOptions{}); err != nil {
			return fmt.Errorf("could not remove container id  %s: %w", container.ID, err)

		}
		logrus.WithField("container", container.ID).Info("remove container")
	}
	return nil
}

func job(client *client.Client, image string, ratio RatioValue) error {
	containers, err := client.ContainerList(context.Background(), types.ContainerListOptions{})
	if err != nil {
		return fmt.Errorf("could not get the list of containers: %w", err)
	}
	candidates := []types.Container{}
	for _, container := range containers {
		if container.Image == image {
			candidates = append(candidates, container)
		}
	}
	for _, candidate := range candidates {
		logrus.WithField("container", candidate.ID).WithField("image", candidate.Image).Debug("found container")
	}
	if len(candidates) == 0 {
		return nil
	}
	rand.Seed(time.Now().Unix())
	containertoCopy := candidates[rand.Intn(len(candidates))]
	if err := copyContainer(client, containertoCopy, ratio.Up); err != nil {
		return err
	}
	if err := deleteContainer(client, candidates, ratio.Down); err != nil {
		return err
	}
	return nil
}

type RatioValue struct {
	Up   uint64
	Down uint64
}

func (r *RatioValue) String() string {
	if r.Up == 0 || r.Down == 0 {
		return "1:1"
	}
	return fmt.Sprintf("%v:%v", r.Up, r.Down)
}

func (r *RatioValue) Set(s string) error {
	vars := strings.Split(s, ":")
	if len(vars) != 2 {
		return errors.New("wrong format")
	}
	up, err := strconv.ParseUint(vars[0], 10, 8)
	if err != nil {
		return err
	}
	down, err := strconv.ParseUint(vars[1], 10, 8)
	if err != nil {
		return err
	}
	r.Up = up
	r.Down = down
	return nil
}

func (r *RatioValue) Type() string {
	return "ratio"
}

func (r RatioValue) isZero() bool {
	return r.Up == 0 || r.Down == 0
}

func main() {

	var ratio RatioValue

	image := flag.StringP("image", "i", "", "containers base on this image will be delete and start again.")
	freq := flag.DurationP("freq", "f", time.Minute, "frequency")
	flag.VarP(&ratio, "ratio", "r", "ratio: x creation: y deletion. eg 1:2, 2:1, 1:1")
	flag.Parse()

	if ratio.isZero() {
		ratio = RatioValue{1, 1}
	}

	sig := make(chan os.Signal)
	signal.Notify(sig, syscall.SIGSTOP, syscall.SIGTERM)

	if *image == "" {
		logrus.Error("could not start application, image argument is empty.")
		flag.Usage()
		os.Exit(1)
	}

	client, err := client.NewEnvClient()
	if err != nil {
		logrus.WithError(err).Error("could not start docker client")
		os.Exit(1)
	}
	defer client.Close()
	for {
		select {
		case <-time.After(*freq):
			if err := job(client, *image, ratio); err != nil {
				logrus.WithError(err).Error("job failed")
			}
		case <-sig:
			logrus.Info("received stop signal")
			return
		}
	}
}
