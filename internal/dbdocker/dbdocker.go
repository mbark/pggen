// Package dbdocker runs a one-off database container so pggen can introspect a
// schema without a database of its own.
//
// It holds the parts that don't care which database is running — building an
// image from a Dockerfile and init scripts, starting and stopping the
// container, capturing its logs, picking a host port. The database-specific
// pieces (the image, the port it listens on, how to tell it is ready) come
// from Config; see internal/pgdocker and internal/chdocker.
package dbdocker

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"time"

	"github.com/docker/docker/api/types/build"
	"github.com/docker/docker/api/types/container"
	dockerClient "github.com/docker/docker/client"
	"github.com/docker/go-connections/nat"

	"github.com/mbark/pggen/internal/errs"
	"github.com/mbark/pggen/internal/ports"
)

// Config describes the container to run.
type Config struct {
	// Name of the database, used in log and error messages.
	Name string
	// Dockerfile is the full contents of the Dockerfile to build. It should
	// copy each entry of InitScripts, by base name, wherever the image expects
	// initialization scripts.
	Dockerfile string
	// InitScripts are host paths to schema files, added to the build context
	// under a numeric prefix so they run in the order given.
	InitScripts []string
	// Env is the container environment, like "POSTGRES_HOST_AUTH_METHOD=trust".
	Env []string
	// Cmd overrides the image command, if set.
	Cmd []string
	// Port the database listens on inside the container, like "5432/tcp".
	Port nat.Port
	// Tmpfs mounts, used to keep the data directory off disk.
	Tmpfs map[string]string
	// WaitReady blocks until the database accepts connections on the host port.
	WaitReady func(ctx context.Context, port ports.Port) error
}

// Client controls a running database container.
type Client struct {
	docker      *dockerClient.Client
	containerID string // container ID if started, empty otherwise
	name        string
	port        ports.Port
}

// Start builds an image from cfg and runs it, returning once the database is
// ready.
func Start(ctx context.Context, cfg Config) (client *Client, mErr error) {
	now := time.Now()
	dockerCl, err := dockerClient.NewClientWithOpts(dockerClient.FromEnv)
	if err != nil {
		return nil, fmt.Errorf("create client: %w", err)
	}
	c := &Client{docker: dockerCl, name: cfg.Name}
	imageID, err := c.buildImage(ctx, cfg)
	slog.DebugContext(ctx, "build image", slog.String("image_id", imageID))
	if err != nil {
		return nil, fmt.Errorf("build image: %w", err)
	}
	containerID, port, err := c.runContainer(ctx, imageID, cfg)
	if err != nil {
		return nil, fmt.Errorf("run container: %w", err)
	}
	c.containerID = containerID
	c.port = port

	// Clean up the container if we fail after starting it. Registered before
	// the log capture so it runs after it — stopping the container also
	// removes it, and its logs with it.
	//
	// The guard is on mErr alone. Guarding on the returned client as well
	// would make this dead code: every failing path returns a nil client, and
	// the successful one leaves mErr nil.
	defer func() {
		if mErr == nil {
			return
		}
		stopCtx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := c.Stop(stopCtx); err != nil {
			slog.ErrorContext(stopCtx, "stop dbdocker client", slog.String("error", err.Error()))
		}
	}()
	// Enrich errors with the container's own logs.
	defer func() {
		if mErr != nil {
			logs, err := c.GetContainerLogs()
			if err != nil {
				mErr = errors.Join(mErr, err)
			} else {
				mErr = fmt.Errorf("%w\nContainer logs for container ID %s\n\n%s", mErr, containerID, logs)
			}
		}
	}()

	if err := cfg.WaitReady(ctx, port); err != nil {
		return nil, fmt.Errorf("wait for %s to be ready: %w", cfg.Name, err)
	}
	slog.DebugContext(ctx, "started docker "+cfg.Name, slog.Duration("start_duration", time.Since(now)))
	return c, nil
}

// Port returns the host port the database is published on.
func (c *Client) Port() ports.Port { return c.port }

// GetContainerLogs returns all stdout and stderr logs for the container.
// Useful to enrich output when pggen fails to start the container.
func (c *Client) GetContainerLogs() (logs string, mErr error) {
	if c.containerID == "" {
		return "", nil
	}
	logsCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	logsR, err := c.docker.ContainerLogs(logsCtx, c.containerID, container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
	})
	if err != nil {
		return "", fmt.Errorf("get container logs: %w", err)
	}
	defer errs.Capture(&mErr, logsR.Close, "close container logs")
	bs, err := io.ReadAll(logsR)
	if err != nil {
		return "", fmt.Errorf("read all container logs: %w", err)
	}
	return string(bs), nil
}

// buildImage builds the image described by cfg, with the init scripts copied
// into the build context.
func (c *Client) buildImage(ctx context.Context, cfg Config) (id string, mErr error) {
	// Prefix each init script so it runs in the order it was given.
	initTarNames := InitScriptNames(cfg.InitScripts)

	// Tar the Dockerfile for the build context.
	tarBuf := &bytes.Buffer{}
	tarW := tar.NewWriter(tarBuf)
	hdr := &tar.Header{Name: "Dockerfile", Size: int64(len(cfg.Dockerfile))}
	if err := tarW.WriteHeader(hdr); err != nil {
		return "", fmt.Errorf("write dockerfile tar header: %w", err)
	}
	if _, err := tarW.Write([]byte(cfg.Dockerfile)); err != nil {
		return "", fmt.Errorf("write dockerfile to tar: %w", err)
	}

	for i, script := range cfg.InitScripts {
		if err := tarInitScript(tarW, script, initTarNames[i]); err != nil {
			return "", fmt.Errorf("tar init file: %w", err)
		}
	}

	tarR := bytes.NewReader(tarBuf.Bytes())
	slog.DebugContext(ctx, "wrote tar dockerfile into buffer")

	opts := build.ImageBuildOptions{Dockerfile: "Dockerfile"}
	resp, err := c.docker.ImageBuild(ctx, tarR, opts)
	if err != nil {
		return "", fmt.Errorf("build %s docker image: %w", cfg.Name, err)
	}
	defer errs.Capture(&mErr, resp.Body.Close, "close image build response")
	response, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read image build response: %w", err)
	}

	imageIDRegexp := regexp.MustCompile(`Successfully built ([a-z0-9]+)`)
	matches := imageIDRegexp.FindSubmatch(response)
	if len(matches) == 0 {
		return "", fmt.Errorf("unable find image ID in docker build output below:\n%s", string(response))
	}
	return string(matches[1]), nil
}

// InitScriptNames returns the name each init script takes in the build
// context, numbered so the image runs them in order.
func InitScriptNames(initScripts []string) []string {
	names := make([]string, len(initScripts))
	for i, script := range initScripts {
		names[i] = fmt.Sprintf("%03d_%s", i, filepath.Base(script))
	}
	return names
}

// tarInitScript writes the contents of an init script into the tar writer
// using tarName.
func tarInitScript(tarW *tar.Writer, script string, tarName string) (mErr error) {
	stat, err := os.Stat(script)
	if err != nil {
		return fmt.Errorf("stat docker init script %s: %w", script, err)
	}
	hdr, err := tar.FileInfoHeader(stat, tarName)
	if err != nil {
		return fmt.Errorf("create tar file header: %w", err)
	}
	hdr.Name = tarName
	hdr.AccessTime = time.Time{}
	hdr.ChangeTime = time.Time{}
	hdr.ModTime = time.Time{}
	if err := tarW.WriteHeader(hdr); err != nil {
		return fmt.Errorf("write init script tar header: %w", err)
	}
	f, err := os.Open(script)
	if err != nil {
		return fmt.Errorf("read init script: %w", err)
	}
	defer errs.Capture(&mErr, f.Close, "close file to tar")
	if _, err := io.Copy(tarW, f); err != nil {
		return fmt.Errorf("copy init script to tar: %w", err)
	}
	return nil
}

// runContainer creates and starts the container, publishing the database port
// on an available host port.
func (c *Client) runContainer(ctx context.Context, imageID string, cfg Config) (string, ports.Port, error) {
	port, err := ports.FindAvailable()
	if err != nil {
		return "", 0, fmt.Errorf("find available port: %w", err)
	}
	containerCfg := &container.Config{
		Image:        imageID,
		Env:          cfg.Env,
		ExposedPorts: nat.PortSet{cfg.Port: struct{}{}},
		Cmd:          cfg.Cmd,
	}
	hostCfg := &container.HostConfig{
		PortBindings: nat.PortMap{
			cfg.Port: []nat.PortBinding{{HostIP: "0.0.0.0", HostPort: strconv.Itoa(port)}},
		},
		Tmpfs: cfg.Tmpfs,
	}
	resp, err := c.docker.ContainerCreate(ctx, containerCfg, hostCfg, nil, nil, "")
	if err != nil {
		return "", 0, fmt.Errorf("create container: %w", err)
	}
	containerID := resp.ID
	slog.DebugContext(ctx, "created "+cfg.Name+" container",
		slog.String("container_id", containerID), slog.Int("port", port))
	if err := c.docker.ContainerStart(ctx, containerID, container.StartOptions{}); err != nil {
		return "", 0, fmt.Errorf("start container: %w", err)
	}
	slog.DebugContext(ctx, "started container", slog.String("container_id", containerID))
	return containerID, port, nil
}

// Stop stops the running container, if any.
func (c *Client) Stop(ctx context.Context) error {
	if c.containerID == "" {
		return nil
	}
	if err := c.docker.ContainerStop(ctx, c.containerID, container.StopOptions{}); err != nil {
		return fmt.Errorf("stop container %s: %w", c.containerID, err)
	}
	err := c.docker.ContainerRemove(ctx, c.containerID, container.RemoveOptions{
		RemoveVolumes: true,
		RemoveLinks:   false,
		Force:         true,
	})
	if err != nil {
		return fmt.Errorf("remove container %s: %w", c.containerID, err)
	}
	return nil
}
