package github

import (
	"context"
	"errors"
	"net/url"
	"reflect"

	"github.com/google/go-github/github"
	redhatcopv1alpha1 "github.com/redhat-cop/gitwebhook-operator/api/v1alpha1"
	"golang.org/x/oauth2"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

type GitHubWebHook struct {
	gitWebhook *redhatcopv1alpha1.GitWebhook
	repository *github.Repository
	webhook    *github.Hook
}

var _ redhatcopv1alpha1.WebHook = &GitHubWebHook{}

func FromGitWebhook(gitwebhook *redhatcopv1alpha1.GitWebhook) *GitHubWebHook {
	return &GitHubWebHook{
		gitWebhook: gitwebhook,
	}
}

func (m *GitHubWebHook) toGitHubWebhook(ctx context.Context) (*github.Hook, error) {
	secret, err := m.gitWebhook.GetWebhookSecret(ctx)
	log := log.FromContext(ctx)
	if err != nil {
		log.Error(err, "unable to retrieve webhook secret")
		return nil, err
	}
	hook := github.Hook{
		Events: m.gitWebhook.Spec.Events,
		Active: &m.gitWebhook.Spec.Active,
		Name:   &m.gitWebhook.Name,
		Config: map[string]interface{}{
			"content_type": m.gitWebhook.Spec.ContentType,
			"insecure_ssl": m.gitWebhook.Spec.InsecureSSL,
			"url":          m.gitWebhook.Spec.WebhookURL,
			"secret":       secret,
		},
	}
	return &hook, nil
}

func (m *GitHubWebHook) getGitHubClient(ctx context.Context) (*github.Client, error) {
	log := log.FromContext(ctx)
	token, err := m.gitWebhook.GetGitCredential(ctx, m.gitWebhook.Spec.GitHub)
	if err != nil {
		log.Error(err, "Unable to retrieve github credential", "secret", m.gitWebhook.Spec.GitHub.GitServerCredentials.Name)
		return nil, err
	}

	ts := oauth2.StaticTokenSource(
		&oauth2.Token{AccessToken: token},
	)
	tc := oauth2.NewClient(ctx, ts)

	git := github.NewClient(tc)

	if m.gitWebhook.Spec.GitHub.GitAPIServerURL != "" {
		git.BaseURL, err = url.Parse(m.gitWebhook.Spec.GitHub.GitAPIServerURL)
		if err != nil {
			log.Error(err, "Unable to parse github url", "url", m.gitWebhook.Spec.GitHub.GitAPIServerURL)
			return nil, err
		}
	}
	return git, nil
}

func (m *GitHubWebHook) getGitHubRepository(ctx context.Context) (*github.Repository, error) {
	if m.repository != nil {
		return m.repository, nil
	}
	log := log.FromContext(ctx)
	git, err := m.getGitHubClient(ctx)
	if err != nil {
		log.Error(err, "unable to create github client")
		return nil, err
	}
	//we try org first
	repositories, _, err := git.Repositories.ListByOrg(ctx, m.gitWebhook.Spec.RepositoryOwner, &github.RepositoryListByOrgOptions{})
	if err != nil {
		log.Error(err, "unable to list repos", "for organization", m.gitWebhook.Spec.RepositoryOwner)
		return nil, err
	}
	for _, repository := range repositories {
		if repository.Name == &m.gitWebhook.Spec.RepositoryName {
			m.repository = repository
			return repository, nil
		}
	}
	//if we get here we didn't find the repo in orgs so we are going to try users
	repositories, _, err = git.Repositories.List(ctx, m.gitWebhook.Spec.RepositoryOwner, &github.RepositoryListOptions{})
	if err != nil {
		log.Error(err, "unable to list repos", "for user", m.gitWebhook.Spec.RepositoryOwner)
		return nil, err
	}
	for _, repository := range repositories {
		if repository.Name == &m.gitWebhook.Spec.RepositoryName {
			m.repository = repository
			return repository, nil
		}
	}
	//if we get here it means that we have not found the repository
	return nil, errors.New("repository not found: " + m.gitWebhook.Spec.RepositoryOwner + "/" + m.gitWebhook.Spec.RepositoryName)
}

func (m *GitHubWebHook) getHook(ctx context.Context) (*github.Hook, bool, error) {
	if m.webhook != nil {
		return m.webhook, true, nil
	}
	log := log.FromContext(ctx)
	git, err := m.getGitHubClient(ctx)
	if err != nil {
		log.Error(err, "unable to create github client")
		return nil, false, err
	}
	repository, err := m.getGitHubRepository(ctx)
	if err != nil {
		log.Error(err, "unable to get repository")
		return nil, false, err
	}
	hook, response, err := git.Repositories.GetHook(ctx, *repository.Owner.Name, *repository.Name, *repository.ID)
	if err != nil {
		log.Error(err, "unable to get hook")
		return nil, false, err
	}
	if response.Status == "404" {
		return nil, false, nil
	}
	m.webhook = hook
	return hook, true, nil
}

func (m *GitHubWebHook) isGitHubEquivalent(ctx context.Context) (bool, error) {
	log := log.FromContext(ctx)
	desiredHook, err := m.toGitHubWebhook(ctx)
	if err != nil {
		log.Error(err, "unable to convert to gitwebhook")
		return false, err
	}

	actualHook, found, err := m.getHook(ctx)
	if err != nil {
		log.Error(err, "error while retrieving current webhook")
		return false, err
	}
	if !found {
		return false, nil
	}

	actualHook.CreatedAt = nil
	actualHook.UpdatedAt = nil
	actualHook.ID = nil

	return reflect.DeepEqual(desiredHook, actualHook), nil

}

func (m *GitHubWebHook) gitHubWebHookReconcile(ctx context.Context) error {
	log := log.FromContext(ctx)
	equivalent, err := m.isGitHubEquivalent(ctx)
	if err != nil {
		log.Error(err, "unable to determine if desired state is equal to actual state")
		return err
	}
	if equivalent {
		return nil
	}
	return m.createOrUpdateGitHubWebHook(ctx)
}

func (m *GitHubWebHook) createOrUpdateGitHubWebHook(ctx context.Context) error {
	log := log.FromContext(ctx)
	_, found, err := m.getHook(ctx)
	if err != nil {
		log.Error(err, "error while retrieving webhook")
		return err
	}
	git, err := m.getGitHubClient(ctx)
	if err != nil {
		log.Error(err, "error get github client")
		return err
	}
	newHook, err := m.toGitHubWebhook(ctx)
	if err != nil {
		log.Error(err, "error to convert to github hook")
		return err
	}
	if !found {
		//we need to create
		_, _, err = git.Repositories.CreateHook(ctx, m.gitWebhook.Spec.RepositoryOwner, m.gitWebhook.Spec.RepositoryName, newHook)
		if err != nil {
			log.Error(err, "unable to create new hook")
			return err
		}
	} else {
		//we need to update
		repository, err := m.getGitHubRepository(ctx)
		if err != nil {
			log.Error(err, "unable to get github repository")
			return err
		}
		_, _, err = git.Repositories.EditHook(ctx, m.gitWebhook.Spec.RepositoryOwner, m.gitWebhook.Spec.RepositoryName, *repository.ID, newHook)
		if err != nil {
			log.Error(err, "unable to update github webhook")
			return err
		}
	}
	return nil
}

func (m *GitHubWebHook) deleteIfExists(ctx context.Context) error {
	log := log.FromContext(ctx)
	hook, found, err := m.getHook(ctx)
	if err != nil {
		log.Error(err, "error while retrieving webhook")
		return err
	}
	if !found {
		return nil
	}
	git, err := m.getGitHubClient(ctx)
	if err != nil {
		log.Error(err, "error get github client")
		return err
	}
	_, err = git.Repositories.DeleteHook(ctx, m.gitWebhook.Spec.RepositoryOwner, m.gitWebhook.Spec.RepositoryName, *hook.ID)
	if err != nil {
		log.Error(err, "unable to delete webhook")
		return err
	}
	return nil
}

func (m *GitHubWebHook) Reconcile(ctx context.Context) error {
	return m.gitHubWebHookReconcile(ctx)
}

func (m *GitHubWebHook) Delete(ctx context.Context) error {
	return m.deleteIfExists(ctx)
}
