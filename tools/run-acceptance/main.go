package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: run-acceptance <terraform|tofu> <provider-host>")
		os.Exit(2)
	}
	engine, err := exec.LookPath(os.Args[1])
	if err != nil {
		fmt.Fprintf(os.Stderr, "find %s: %v\n", os.Args[1], err)
		os.Exit(1)
	}
	engine, err = filepath.Abs(engine)
	if err != nil {
		fmt.Fprintf(os.Stderr, "resolve %s: %v\n", os.Args[1], err)
		os.Exit(1)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 31*time.Minute)
	defer cancel()
	command := exec.CommandContext(ctx, "go", "test", "-v", "-timeout", "30m", "./internal/provider", "-run", "^TestAcc")
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	command.Stdin = os.Stdin
	command.Env = append(os.Environ(),
		"TF_ACC=1",
		"TF_ACC_PROVIDER_HOST="+os.Args[2],
		"TF_ACC_PROVIDER_NAMESPACE=vappcloud",
		"TF_ACC_TERRAFORM_PATH="+engine,
	)
	if err := command.Run(); err != nil {
		var exitError *exec.ExitError
		if errors.As(err, &exitError) {
			os.Exit(exitError.ExitCode())
		}
		fmt.Fprintf(os.Stderr, "run acceptance tests: %v\n", err)
		os.Exit(1)
	}
}
