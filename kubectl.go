package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

type Pod struct {
	Name        string
	Namespace   string
	Status      string
	Containers  []string
	LaravelLike bool
}

func (p Pod) Key() string {
	return p.Namespace + "/" + p.Name
}

type PodCommand struct {
	Label   string
	Command []string
	Logs    bool
}

type KubectlClient struct {
	config Config
}

const kubectlTimeout = 10 * time.Second

var ansiPattern = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]`)

type podList struct {
	Items []struct {
		Metadata struct {
			Name      string `json:"name"`
			Namespace string `json:"namespace"`
		} `json:"metadata"`
		Status struct {
			Phase string `json:"phase"`
		} `json:"status"`
		Spec struct {
			Containers []struct {
				Name string `json:"name"`
			} `json:"containers"`
		} `json:"spec"`
	} `json:"items"`
}

func (c *KubectlClient) ListNamespaces() ([]string, error) {
	namespaces := c.config.LaravelNamespaceNames()
	sortStrings(namespaces)
	if len(namespaces) == 0 {
		return nil, errNoNamespaces
	}

	return namespaces, nil
}

func (c *KubectlClient) ListPods(namespace string) ([]Pod, error) {
	stdout, stderr, err := c.runKubectl("get", "pods", "-n", namespace, "-o", "json")
	if err != nil {
		return nil, formatKubectlError(fmt.Sprintf("kubectl get pods failed for namespace %s", namespace), err, stderr)
	}

	var raw podList
	if err := json.Unmarshal(stdout, &raw); err != nil {
		return nil, fmt.Errorf("decode kubectl output: %w", err)
	}

	pods := make([]Pod, 0, len(raw.Items))
	for _, item := range raw.Items {
		containers := make([]string, 0, len(item.Spec.Containers))
		for _, c := range item.Spec.Containers {
			containers = append(containers, c.Name)
		}
		pods = append(pods, Pod{
			Name:        item.Metadata.Name,
			Namespace:   item.Metadata.Namespace,
			Status:      item.Status.Phase,
			Containers:  containers,
			LaravelLike: c.config.IsLaravelNamespace(namespace) || looksLikeLaravelPod(item.Metadata.Name, containers),
		})
	}

	sortPods(pods)
	if len(pods) == 0 {
		return nil, errNoPods
	}

	return pods, nil
}

func (c *KubectlClient) RunCommandAcrossPods(pods []Pod, podCommand PodCommand) (string, error) {
	var output strings.Builder
	var errs []string

	for _, pod := range pods {
		output.WriteString(fmt.Sprintf("=== %s/%s ===\n", pod.Namespace, pod.Name))
		var result string
		var err error
		if podCommand.Logs {
			result, err = c.RunLogs(pod, podCommand.Command)
		} else {
			result, err = c.RunCommand(pod, podCommand.Command)
		}
		if result != "" {
			output.WriteString(result)
			if !strings.HasSuffix(result, "\n") {
				output.WriteString("\n")
			}
		}
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s/%s: %v", pod.Namespace, pod.Name, err))
		}
		output.WriteString("\n")
	}

	if len(errs) > 0 {
		return output.String(), errors.New(strings.Join(errs, "; "))
	}

	return output.String(), nil
}

func (c *KubectlClient) RunCommand(pod Pod, command []string) (string, error) {
	args := []string{"exec", "-n", pod.Namespace, pod.Name, "--"}
	args = append(args, command...)

	return c.runAndSanitize(args...)
}

func (c *KubectlClient) RunLogs(pod Pod, command []string) (string, error) {
	args := []string{"logs", "-n", pod.Namespace, pod.Name}
	args = append(args, command...)

	return c.runAndSanitize(args...)
}

func sanitizeOutput(output string) string {
	output = strings.ReplaceAll(output, "\r\n", "\n")
	output = strings.ReplaceAll(output, "\r", "\n")
	output = ansiPattern.ReplaceAllString(output, "")
	return strings.TrimSpace(output)
}

func (c *KubectlClient) runKubectl(args ...string) ([]byte, []byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), kubectlTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "kubectl", args...)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if ctx.Err() == context.DeadlineExceeded {
		return stdout.Bytes(), stderr.Bytes(), fmt.Errorf("kubectl timed out after %s", kubectlTimeout)
	}

	return stdout.Bytes(), stderr.Bytes(), err
}

func (c *KubectlClient) runAndSanitize(args ...string) (string, error) {
	stdout, stderr, err := c.runKubectl(args...)
	output := sanitizeOutput(joinCommandOutput(stdout, stderr))
	if err != nil {
		return output, err
	}

	return output, nil
}

func joinCommandOutput(stdout, stderr []byte) string {
	combined := strings.TrimSpace(string(stdout))
	if serr := strings.TrimSpace(string(stderr)); serr != "" {
		if combined != "" {
			combined += "\n"
		}
		combined += serr
	}

	return combined
}

func formatKubectlError(prefix string, err error, stderr []byte) error {
	message := strings.TrimSpace(string(stderr))
	if message == "" {
		return fmt.Errorf("%s: %w", prefix, err)
	}
	return fmt.Errorf("%s: %w\n%s", prefix, err, message)
}

func looksLikeLaravelPod(name string, containers []string) bool {
	joined := strings.ToLower(name + " " + strings.Join(containers, " "))
	keywords := []string{
		"laravel",
		"artisan",
		"php",
		"fpm",
		"horizon",
		"queue",
		"octane",
		"nginx",
	}

	for _, keyword := range keywords {
		if strings.Contains(joined, keyword) {
			return true
		}
	}

	return false
}
