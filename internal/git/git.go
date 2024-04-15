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
package git

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"log"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/transport/ssh"
	"golang.org/x/exp/maps"
)

type ErrNotFound struct {
	error
	Key string
}

// GetRepository opens a git repository from the given path. Returns
// ErrRepositoryNotExists if no repository is found.
func GetRepository(path string) (*git.Repository, error) {
	return git.PlainOpenWithOptions(path, &git.PlainOpenOptions{
		DetectDotGit: true,
	})
}

// PushZettel creates and pushes a commit for a newly added zettel.
func PushZettel(zettelId string, repo *git.Repository) (*object.Commit, error) {
	username, email, err := getUserInfo()
	handlePushZettelErr(err)

	worktree, err := repo.Worktree()
	handlePushZettelErr(err)

	// Worktree status contains repo changes as keys in a map
	status, err := worktree.Status()
	handlePushZettelErr(err)

	changes := maps.Keys(status)
	if len(changes) == 0 {
		return nil, errors.New("git: no changes detected")
	}

	// Find the change for this zettel (avoid unrelated changes)
	var change string
	for i := range len(changes) {
		if strings.Contains(changes[i], zettelId) {
			change = changes[i]
			break
		}
	}

	if change == "" {
		errmsg := fmt.Sprintf("git: no changes detected for zettel %s", zettelId)
		return nil, errors.New(errmsg)
	}

	// Stage the new zettel file
	_, err = worktree.Add(change)
	handlePushZettelErr(err)

	// Create commit for new zettel
	commitMsg := fmt.Sprintf("Add zettel %s", zettelId)
	commit, err := worktree.Commit(commitMsg, &git.CommitOptions{
		Author: &object.Signature{
			Name:  username,
			Email: email,
			When:  time.Now(),
		},
	})
	handlePushZettelErr(err)

	// Use SSH to push the commit to the remote
	authMethod, err := ssh.DefaultAuthBuilder("git")
	handlePushZettelErr(err)

	err = repo.Push(&git.PushOptions{
		RemoteName: "origin",
		Auth:       authMethod,
	})
	handlePushZettelErr(err)

	// Return commit details to caller
	commitObj, err := repo.CommitObject(commit)
	handlePushZettelErr(err)

	return commitObj, nil
}

func handlePushZettelErr(err error) {
	if err == nil {
		return
	}

	log.Fatal(err)
}

func getUserInfo() (name string, email string, err error) {
	username, userErr := localGitConfig("user.name")
	email, emailErr := localGitConfig("user.email")
	if userErr != nil || emailErr != nil {
		username, userErr = globalGitConfig("user.name")
		email, emailErr = globalGitConfig("user.email")
	}

	if userErr != nil || emailErr != nil {
		err = errors.New("git: username and email not found")
		return "", "", err
	}

	return username, email, nil
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
