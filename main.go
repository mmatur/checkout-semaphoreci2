package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/containous/flaeg"
	"github.com/google/go-github/github"
	"github.com/ldez/go-git-cmd-wrapper/checkout"
	"github.com/ldez/go-git-cmd-wrapper/clone"
	"github.com/ldez/go-git-cmd-wrapper/git"
	"github.com/ldez/go-git-cmd-wrapper/reset"
	"github.com/ogier/pflag"
	"golang.org/x/oauth2"
)

// Config holds configuration.
type Config struct {
	Owner       string `description:"Repository owner"`
	Repo        string `description:"Repository name"`
	GithubToken string `description:"Github Token"`
	URL         string `description:"The URL of the GitHub repository used in the current Semaphore 2.0 project."`
	Branch      string `description:"The name of the GitHub branch that is used in the current job."`
	SHA         string `description:"The current revision of code that the pipeline is using."`
	Directory   string `description:"Name of the directory that contains the files of the GitHub repository of the current Semaphore 2.0 project"`
}

// HasLabelConfig holds has-label configuration.
type HasLabelConfig struct {
	*Config
	Label string `description:"Required label"`
}

// NoOption empty struct.
type NoOption struct{}

func main() {
	defaultCfg := &Config{}
	defaultPointerCfg := &Config{}

	rootCmd := &flaeg.Command{
		Name:                  "checkout-semaphoreci2",
		Description:           "Checkout SemaphoreCI",
		Config:                defaultCfg,
		DefaultPointersConfig: defaultPointerCfg,
		Run: func() error {
			return rootRun(defaultCfg)
		},
	}

	flag := flaeg.New(rootCmd, os.Args[1:])

	hasLabelCfg := &HasLabelConfig{
		Config: &Config{},
	}
	hasLabelPointerCfg := &HasLabelConfig{
		Config: &Config{},
	}
	// hasLabel
	hasLabelCmd := &flaeg.Command{
		Name:                  "has-label",
		Description:           "Check if PR has label",
		Config:                hasLabelCfg,
		DefaultPointersConfig: hasLabelPointerCfg,
		Run: func() error {
			return hasLabelRun(hasLabelCfg)
		},
	}

	flag.AddCommand(hasLabelCmd)

	isPRCfg := &Config{}
	isPRPointerCfg := &Config{}

	// isPR
	isPRCmd := &flaeg.Command{
		Name:                  "is-pr",
		Description:           "Check if its a PR",
		Config:                isPRCfg,
		DefaultPointersConfig: isPRPointerCfg,
		Run: func() error {
			return isPRRun(isPRCfg)
		},
	}

	flag.AddCommand(isPRCmd)

	// version
	versionCmd := &flaeg.Command{
		Name:                  "version",
		Description:           "Display the version.",
		Config:                &NoOption{},
		DefaultPointersConfig: &NoOption{},
		Run: func() error {
			DisplayVersion()
			return nil
		},
	}

	flag.AddCommand(versionCmd)

	// Run command
	if err := flag.Run(); err != nil && err != pflag.ErrHelp {
		log.Printf("Error: %v\n", err)
	}
}

func isPRRun(config *Config) error {
	defaultConfig(config)

	if err := validate(config); err != nil {
		return err
	}

	_, err := getPR(config)
	if err != nil {
		log.Println("It's not a PR")
		return nil
	}

	log.Println("yes")

	return nil
}

func hasLabelRun(config *HasLabelConfig) error {
	defaultConfig(config.Config)

	if err := validate(config.Config); err != nil {
		return err
	}

	if err := required(config.Label, "label"); err != nil {
		return err
	}

	pr, err := getPR(config.Config)
	if err != nil {
		log.Println("It's not a PR")
		return nil
	}

	if !hasLabel(pr, config.Label) {
		return fmt.Errorf("PR has no label %s", config.Label)
	}

	log.Println("yes")
	return nil
}

func hasLabel(pr *github.PullRequest, label string) bool {
	for _, value := range pr.Labels {
		if value.GetName() == label {
			return true
		}
	}

	return false
}

func rootRun(config *Config) error {
	defaultConfig(config)

	if err := validate(config); err != nil {
		return err
	}

	if strings.Contains(config.Branch, "pull-request-") {
		err := checkoutPR(config)
		if err != nil {
			return err
		}

		return nil
	}

	return cloneAndCheckout(config.URL, config.Directory, config.Branch, config.SHA)
}

func defaultConfig(config *Config) {
	if config.URL == "" {
		config.URL = os.Getenv("SEMAPHORE_GIT_BRANCH")
	}

	if config.Branch == "" {
		config.Branch = os.Getenv("SEMAPHORE_GIT_BRANCH")
	}

	if config.Directory == "" {
		config.Directory = os.Getenv("SEMAPHORE_GIT_DIR")
	}

	if config.SHA == "" {
		config.SHA = os.Getenv("SEMAPHORE_GIT_SHA")
	}

	if config.GithubToken == "" {
		config.GithubToken = os.Getenv("GITHUB_TOKEN")
	}
}

func checkoutPR(config *Config) error {
	pr, err := getPR(config)
	if err != nil {
		return err
	}

	if pr != nil {
		if pr.GetHead() == nil || pr.GetHead().GetRepo() == nil {
			return fmt.Errorf("unable to get head of PR %d", pr.GetID())
		}

		gitURL := makeRepositoryURL(pr.GetHead().GetRepo().GetGitURL(), config.GithubToken)
		return cloneAndCheckout(gitURL, config.Directory, pr.GetHead().GetRef(), config.SHA)
	}

	return nil
}

func getPR(config *Config) (*github.PullRequest, error) {
	s := strings.Split(config.Branch, "pull-request-")
	if len(s) == 2 {
		ID, err := strconv.Atoi(s[1])
		if err != nil {
			return nil, err
		}

		ctx := context.Background()
		client := createGhClient(ctx, config)
		pr, _, err := client.PullRequests.Get(ctx, config.Owner, config.Repo, ID)
		if err != nil {
			return nil, err
		}
		return pr, nil
	}

	return nil, fmt.Errorf("unable to get PR number for branch %s", config.Branch)
}

func cloneAndCheckout(url string, directory string, branch string, sha string) error {
	currentDirectory, err := os.Getwd()
	if err != nil {
		return err
	}
	checkoutDirectory := filepath.Join(currentDirectory, directory)

	if _, err = os.Stat(checkoutDirectory); !os.IsNotExist(err) {
		err = os.RemoveAll(checkoutDirectory)
		if err != nil {
			return err
		}
	}

	output, err := git.Clone(clone.Repository(url), clone.Directory(directory), git.Debugger(true))
	log.Println(output)
	if err != nil {
		return fmt.Errorf("failed to clone url %s in  %s: %v", url, directory, err)
	}

	err = os.Chdir(checkoutDirectory)
	if err != nil {
		return err
	}

	output, err = git.Checkout(checkout.Branch(branch), git.Debugger(true))
	log.Println(output)
	if err != nil {
		return fmt.Errorf("failed to checkout SHA %s: %v", sha, err)
	}

	output, err = git.Reset(reset.Commit(sha), reset.Hard, git.Debugger(true))
	log.Println(output)
	if err != nil {
		return fmt.Errorf("failed to checkout SHA %s: %v", sha, err)
	}

	return nil
}

func makeRepositoryURL(url string, token string) string {
	prefix := "https://"
	if len(token) > 0 {
		prefix += token + "@"
	}
	return strings.Replace(url, "git://", prefix, -1)
}

func createGhClient(ctx context.Context, config *Config) *github.Client {
	tc := oauth2.NewClient(ctx, oauth2.StaticTokenSource(
		&oauth2.Token{AccessToken: config.GithubToken},
	))
	return github.NewClient(tc)
}

func validate(config *Config) error {
	if err := required(config.Owner, "owner"); err != nil {
		return err
	}

	if err := required(config.Repo, "repo"); err != nil {
		return err
	}

	if err := required(config.URL, "url"); err != nil {
		return err
	}

	if err := required(config.Branch, "branch"); err != nil {
		return err
	}

	if err := required(config.SHA, "sha"); err != nil {
		return err
	}

	if err := required(config.Directory, "directory"); err != nil {
		return err
	}

	return required(config.GithubToken, "githubtoken")
}

func required(field string, fieldName string) error {
	if len(field) == 0 {
		return fmt.Errorf("option %s is mandatory", fieldName)
	}
	return nil
}
