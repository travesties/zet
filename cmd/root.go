/*
Copyright © 2024 Travis Hunt travishuntt@proton.me

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
GNU General Public License for more details.

You should have received a copy of the GNU General Public License
along with this program. If not, see <http://www.gnu.org/licenses/>.
*/
package cmd

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/transport/ssh"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"golang.org/x/exp/maps"
)

type Zettel struct {
	Id   string
	Path string
	File *os.File
}

type ErrNotFound struct {
	error
	Key string
}

var cfgFile string

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   "zet",
	Short: "Create zettels (entries) in your zettelkasten (slip box)",
	Long: `Zet is a utility that allows you to quickly add a zettel (slip) to your
zettelkasten (slip box).

When configuring zet, you provide it with a path to your zettelkasten, preferably
a github repository. Zettels you create will be placed at this path. For example,
creating a zet at /path/to/your/zettelkasten/content will create an entry like this:

	/path/to/your/zettelkasten/
		-> content/
			-> 20060102150405/
				-> README.md

Zet creates a directory with a unique ID for a name, which ensures no zettel naming
conflicts. Inside the zettel dir is a README.md that contains the content of the zettel.

The generate ID string is a UTC timestamp, in the format YYYYMMDDHHMMSS, from when the
zettel was created.

Zettels are created as Markdown files. Support for Markdown is ubiquitous and it is
highly searchable. More info here: https://rwx.gg/lang/md/
`,
	Run: func(cmd *cobra.Command, args []string) {
		// Find path provided by config file (perhaps a --content flag could be used as well)
		if !viper.IsSet("content.path") {
			log.Fatalln("zet config: key [content.path] is not set")
		}

		// Verify that the content path exists. Throw error if it doesn't.
		contentPath := viper.GetString("content.path")
		_, err := os.Stat(contentPath)
		if errors.Is(err, os.ErrNotExist) {
			log.Fatalf("zet create: %v\n", err)
		}

		// TODO: Create file from default template

		zettel, err := createZettel(contentPath)
		if err != nil {
			log.Fatalf("zet create: %v\n", err)
		}

		editor := viper.GetString("editor")
		editCmd := exec.Command(editor, zettel.File.Name())
		editCmd.Stdout = os.Stdout
		editCmd.Stdin = os.Stdin
		editCmd.Stderr = os.Stderr

		err = editCmd.Run()
		if err != nil {
			log.Fatalf("zet edit: %v\n", err)
		}

		fmt.Printf("zet created: %v\n", zettel.File.Name())

		// Attempt to find git repo within parent dirs
		repo, err := git.PlainOpenWithOptions(zettel.Path, &git.PlainOpenOptions{
			DetectDotGit: true,
		})
		if err != nil {
			// Bail if not a git repository
			return
		}

		doPush := getConfirmation("Commit and push zettel?")
		if !doPush {
			return
		}

		// Get git username and email. Try local first, and default to global.
		username, userErr := localGitConfig("user.name")
		email, emailErr := localGitConfig("user.email")
		if userErr != nil || emailErr != nil {
			username, userErr = globalGitConfig("user.name")
			email, emailErr = globalGitConfig("user.email")
		}

		if username == "" || email == "" {
			log.Fatal("git config: missing user.name and user.email")
		}

		wtree, err := repo.Worktree()
		checkIfError(err)

		// Get worktree status to get staging path for new zettel
		status, err := wtree.Status()
		checkIfError(err)

		detectedChanges := maps.Keys(status)
		if len(detectedChanges) == 0 {
			log.Fatal("git: no changes detected")
		}

		// Find change path for this zettel (there could be unrelated changes)
		var change string
		for i := range len(detectedChanges) {
			if strings.Contains(detectedChanges[i], zettel.Id) {
				change = detectedChanges[i]
				break
			}
		}

		if change == "" {
			log.Fatalf("git: detected no changes for zettel %s", zettel.Id)
		}

		_, err = wtree.Add(change)
		checkIfError(err)

		commitMsg := fmt.Sprintf("Add zettel %s", zettel.Id)
		commit, err := wtree.Commit(commitMsg, &git.CommitOptions{
			Author: &object.Signature{
				Name:  username,
				Email: email,
				When:  time.Now(),
			},
		})
		checkIfError(err)

		commitObj, err := repo.CommitObject(commit)
		checkIfError(err)

		authMethod, err := ssh.DefaultAuthBuilder("git")
		checkIfError(err)

		err = repo.Push(&git.PushOptions{
			RemoteName: "origin",
			Auth:       authMethod,
		})
		checkIfError(err)

		fmt.Printf("\n%v", commitObj)
		fmt.Println("\npush complete")
	},
}

func checkIfError(err error) {
	if err == nil {
		return
	}

	log.Fatal(err)
}

// Creates a zettel entry at the given path
func createZettel(path string) (*Zettel, error) {
	isosec := generateIsosec()
	wrapperDir := fmt.Sprintf("%v/%v", path, isosec)
	err := os.Mkdir(wrapperDir, 0777)
	if err != nil {
		return nil, err
	}

	zettelPath := fmt.Sprintf("%v/README.md", wrapperDir)
	zettelFile, err := os.Create(zettelPath)
	if err != nil {
		os.RemoveAll(wrapperDir)
		return nil, err
	}
	defer zettelFile.Close()

	// pre-fill id into title string
	zettelFile.WriteString(fmt.Sprintf("# %v", isosec))

	zettel := Zettel{Id: isosec, File: zettelFile, Path: wrapperDir}
	return &zettel, nil
}

// Generates UTC timestamp in the format "YYYYMMDDHHMMSS"
// https://pkg.go.dev/time#example-Time.Format
func generateIsosec() string {
	return time.Now().UTC().Format("20060102150405")
}

func getConfirmation(prompt string) bool {
	reader := bufio.NewReader(os.Stdin)

	for {
		fmt.Printf("%s [y/n]: ", prompt)

		response, err := reader.ReadString('\n')
		if err != nil {
			log.Fatal(err)
		}

		response = strings.ToLower(strings.TrimSpace(response))

		if response == "y" || response == "yes" {
			return true
		} else if response == "n" || response == "no" {
			return false
		}
	}
}

func execGitConfig(args ...string) (string, error) {
	gitArgs := append([]string{"config", "--get", "--null"}, args...)
	var stdout bytes.Buffer
	cmd := exec.Command("git", gitArgs...)
	cmd.Stdout = &stdout
	cmd.Stderr = io.Discard

	err := cmd.Run()
	if exitError, ok := err.(*exec.ExitError); ok {
		if waitStatus, ok := exitError.Sys().(syscall.WaitStatus); ok {
			if waitStatus.ExitStatus() == 1 {
				return "", &ErrNotFound{Key: args[len(args)-1]}
			}
		}
		return "", err
	}

	return strings.TrimRight(stdout.String(), "\000"), nil
}

func globalGitConfig(key string) (string, error) {
	return execGitConfig("--global", key)
}

func localGitConfig(key string) (string, error) {
	return execGitConfig("--local", key)
}

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() {
	err := rootCmd.Execute()
	if err != nil {
		os.Exit(1)
	}
}

func init() {
	cobra.OnInitialize(initConfig)

	// Here you will define your flags and configuration settings.
	// Cobra supports persistent flags, which, if defined here,
	// will be global for your application.

	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is $XDG_CONFIG_HOME/zet/zet.toml)")

	// Cobra also supports local flags, which will only run
	// when this action is called directly.
	//rootCmd.Flags().BoolP("toggle", "t", false, "Help message for toggle")
}

// initConfig reads in config file and ENV variables if set.
func initConfig() {
	if cfgFile != "" {
		// Use config file from the flag.
		viper.SetConfigFile(cfgFile)
	} else {
		// Find config directory.
		config, err := os.UserConfigDir()
		cobra.CheckErr(err)

		// Search config in home directory with name ".zet" (without extension).
		zetConfigPath := fmt.Sprintf("%v/zet", config)
		viper.AddConfigPath(zetConfigPath)
		viper.SetConfigType("toml")
		viper.SetConfigName("zet")
	}

	viper.SetDefault("editor", "vi")

	viper.AutomaticEnv() // read in environment variables that match

	// If a config file is found, read it in.
	if err := viper.ReadInConfig(); err != nil {
		fmt.Fprintln(os.Stderr, "zet config: failed to read config file")
	}
}
