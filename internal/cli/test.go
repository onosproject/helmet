// SPDX-FileCopyrightText: 2020-present Open Networking Foundation <info@opennetworking.org>
//
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	petname "github.com/dustinkirkland/golang-petname"
	"github.com/fatih/color"
	"github.com/onosproject/helmit/internal/build"
	"github.com/onosproject/helmit/internal/logging"
	"math/rand"
	"os"
	"path/filepath"
	"time"

	"github.com/onosproject/helmit/internal/job"

	"github.com/onosproject/helmit/pkg/test"
	"github.com/spf13/cobra"
	corev1 "k8s.io/api/core/v1"
)

var (
	successColor = color.New(color.FgGreen)
	failureColor = color.New(color.FgRed, color.Bold)
)

const (
	successIcon = "✓"
	failureIcon = "✗"
)

func init() {
	rand.Seed(time.Now().UnixNano())
}

const testExamples = `
  # Run tests packaged in a Docker image.
  helmit test --image atomix/kubernetes-tests:latest

  # Run tests by referencing a command package and providing a context.
  # The specified context will be loaded into the test pod as the current working directory.
  helmit test ./cmd/tests --context ./charts

  # Run tests in a specific namespace.
  helmit test ./cmd/tests -n integration-tests

  # Run a test suite by name.
  helmit test ./cmd/tests -c ./charts --suite atomix

  # Run a single test by name.
  helmit test ./cmd/tests -c ./charts --suite atomix --test TestMap

  # Override Helm chart values with flags.
  # Value overrids must be namespaced with the name of the release to which to apply the value.
  helmit test ./cmd/tests -c ./charts --set atomix-controller.image=atomix/atomix-controller:latest --set atomix-raft.replicas=3 --suite atomix

  # Override Helm chart values with values files.
  # Values files must be key/value pairs where the key is the Helm release name and the value the path to the file.
  helmit test ./cmd/tests -c ./charts -f atomix-controller=./atomix-controller.yaml --suite atomix
`

func getTestCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "test",
		Aliases: []string{"tests"},
		Short:   "Run tests on Kubernetes",
		Example: testExamples,
		Args:    cobra.ArbitraryArgs,
		RunE:    runTestCommand,
	}
	cmd.Flags().StringP("namespace", "n", "default", "the namespace in which to run the tests")
	cmd.Flags().Bool("create-namespace", false, "whether to create the namespace when running the test")
	cmd.Flags().String("service-account", "", "the name of the service account to use to run test pods")
	cmd.Flags().StringP("context", "c", "", "the test context")
	cmd.Flags().StringP("image", "i", "", "the test image to run")
	cmd.Flags().String("image-pull-policy", string(corev1.PullIfNotPresent), "the Docker image pull policy")
	cmd.Flags().StringArrayP("values", "f", []string{}, "release values paths")
	cmd.Flags().StringArray("set", []string{}, "chart value overrides")
	cmd.Flags().StringSliceP("suite", "s", []string{"TestSuite$"}, "regular expressions to filter the names of test suite(s)")
	cmd.Flags().StringSliceP("test", "t", []string{}, "the name of the test method to run")
	cmd.Flags().BoolP("parallel", "p", false, "whether to run test tests in parallel")
	cmd.Flags().Duration("timeout", 10*time.Minute, "test timeout")
	cmd.Flags().Int("iterations", 1, "number of iterations")
	cmd.Flags().Bool("until-failure", false, "run until an error is detected")
	cmd.Flags().Bool("no-teardown", false, "do not tear down clusters following tests")
	cmd.Flags().StringSlice("secret", []string{}, "secrets to pass to the kubernetes pod")
	cmd.Flags().StringToStringP("args", "a", map[string]string{}, "a mapping of named test arguments")
	return cmd
}

func runTestCommand(cmd *cobra.Command, args []string) error {
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true

	verbose, _ := cmd.Flags().GetBool("verbose")
	namespace, _ := cmd.Flags().GetString("namespace")
	createNamespace, _ := cmd.Flags().GetBool("create-namespace")
	serviceAccount, _ := cmd.Flags().GetString("service-account")
	contextPath, _ := cmd.Flags().GetString("context")
	image, _ := cmd.Flags().GetString("image")
	files, _ := cmd.Flags().GetStringArray("values")
	sets, _ := cmd.Flags().GetStringArray("set")
	suites, _ := cmd.Flags().GetStringSlice("suite")
	testNames, _ := cmd.Flags().GetStringSlice("test")
	parallel, _ := cmd.Flags().GetBool("parallel")
	timeout, _ := cmd.Flags().GetDuration("timeout")
	imagePullPolicy, _ := cmd.Flags().GetString("image-pull-policy")
	pullPolicy := corev1.PullPolicy(imagePullPolicy)
	iterations, _ := cmd.Flags().GetInt("iterations")
	untilFailure, _ := cmd.Flags().GetBool("until-failure")
	noTeardown, _ := cmd.Flags().GetBool("no-teardown")
	secretsArray, _ := cmd.Flags().GetStringSlice("secret")
	testArgs, _ := cmd.Flags().GetStringToString("args")

	if untilFailure {
		iterations = -1
	}

	// Either a command package or image must be specified
	pkgPaths := args
	if len(pkgPaths) == 0 && image == "" {
		return errors.New("must specify either a test package or --image to run")
	}

	valueFiles, err := parseFiles(files)
	if err != nil {
		return err
	}

	values, err := parseOverrides(sets)
	if err != nil {
		return err
	}

	secrets, err := parseSecrets(secretsArray)
	if err != nil {
		return err
	}

	// Generate a unique test ID
	testID := petname.Generate(2, "-")

	var executable string
	if len(pkgPaths) > 0 {
		step := logging.NewStep(testID, "Preparing artifacts")
		step.Start()
		executable = filepath.Join(os.TempDir(), "helmit", testID)
		defer func() {
			_ = os.RemoveAll(executable)
		}()
		image = defaultRunnerImage
		if err := build.Tests(step, suites...).Build(executable, pkgPaths...); err != nil {
			step.Fail(err)
			return err
		}
		step.Complete()
	}

	config := test.Config{
		Namespace:  namespace,
		Suites:     suites,
		Tests:      testNames,
		Parallel:   parallel,
		Iterations: iterations,
		Values:     values,
		Verbose:    verbose,
		Args:       testArgs,
		Timeout:    timeout,
		NoTeardown: noTeardown,
	}

	if contextPath != "" {
		config.Context = filepath.Join(job.HomeDir, job.ContextDir)
	}

	if len(valueFiles) > 0 {
		config.ValueFiles = make(map[string][]string)
		for release, releaseFiles := range valueFiles {
			var absFiles []string
			for _, releaseFile := range releaseFiles {
				absFiles = append(absFiles, filepath.Join(job.HomeDir, filepath.Base(releaseFile)))
			}
			config.ValueFiles[release] = absFiles
		}
	}

	job := job.Job[test.Config]{
		ID:              testID,
		Namespace:       namespace,
		CreateNamespace: createNamespace,
		DeleteNamespace: createNamespace && !noTeardown,
		ServiceAccount:  serviceAccount,
		Image:           image,
		ImagePullPolicy: pullPolicy,
		Executable:      executable,
		Context:         contextPath,
		ValueFiles:      valueFiles,
		Secrets:         secrets,
		Config:          config,
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	step := logging.NewStep(testID, "Setting up tests")
	step.Start()
	if err := job.Create(ctx, step); err != nil {
		step.Fail(err)
		return err
	}
	step.Complete()

	step = logging.NewStep(testID, "Running tests")
	step.Start()

	// Open a log stream for the job
	stream, err := job.GetLogs(context.Background())
	if err != nil {
		return err
	}
	defer stream.Close()

	scanner := bufio.NewScanner(stream)
	for scanner.Scan() {
		fmt.Fprintf(cmd.OutOrStdout(), "    %s\n", scanner.Text())
	}

	// Get the exit code for the job.
	_, code, err := job.GetStatus(ctx)
	if err != nil {
		return err
	}
	step.Complete()

	step = logging.NewStep(testID, "Cleaning up tests")
	step.Start()
	if err := job.Delete(ctx, step); err != nil {
		step.Fail(err)
		return err
	}
	step.Complete()

	if code == 0 {
		_, _ = successColor.Fprintf(cmd.OutOrStdout(), "%s Tests passed!\n", successIcon)
	} else {
		_, _ = failureColor.Fprintf(cmd.OutOrStdout(), "%s Tests failed!\n", failureIcon)
	}
	os.Exit(code)
	return nil
}
