package github

import (
	"context"
	"net/url"
	"reflect"

	"github.com/google/go-github/v48/github"
	redhatcopv1alpha1 "github.com/redhat-cop/gitwebhook-operator/api/v1alpha1"
	"golang.org/x/oauth2"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

type GitHubWebHook struct {
	gitWebhook *redhatcopv1alpha1.GitWebhook
	git        *github.Client
}

var web string = "web"

var _ redhatcopv1alpha1.WebHook = &GitHubWebHook{}

func FromGitWebhook(gitwebhook *redhatcopv1alpha1.GitWebhook) *GitHubWebHook {
	return &GitHubWebHook{
		gitWebhook: gitwebhook,
	}
}

func (m *GitHubWebHook) toWebhook(ctx context.Context) (*github.Hook, error) {
	secret, err := m.gitWebhook.GetWebhookSecret(ctx)
	log := log.FromContext(ctx)
	if err != nil {
		log.Error(err, "unable to retrieve webhook secret")
		return nil, err
	}
	var insecure string = "0"
	if m.gitWebhook.Spec.InsecureSSL {
		insecure = "1"
	}
	hook := github.Hook{
		Events: m.gitWebhook.Spec.Events,
		Active: &m.gitWebhook.Spec.Active,
		Name:   &web,
		Config: map[string]interface{}{
			"content_type": m.gitWebhook.Spec.ContentType,
			"insecure_ssl": insecure,
			"url":          m.gitWebhook.Spec.WebhookURL,
			"secret":       secret,
		},
	}
	return &hook, nil
}

func (m *GitHubWebHook) getClient(ctx context.Context) (*github.Client, error) {
	if m.git != nil {
		return m.git, nil
	}
	log := log.FromContext(ctx)
	token, err := m.gitWebhook.GetGitCredential(ctx, m.gitWebhook.Spec.GitHub)
	if err != nil {
		log.Error(err, "Unable to retrieve github credential", "secret", m.gitWebhook.Spec.GitHub.GitServerCredentials.Name)
		return nil, err
	}

	ts := oauth2.StaticTokenSource(
		&oauth2.Token{AccessToken: token},
	)
	//debug client, might be useful
	//tc := &oauth2.Transport{Source: ts, Base: dbg.New()}
	//git := github.NewClient(&http.Client{Transport: tc})

	//production client
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

func (m *GitHubWebHook) getHook(ctx context.Context) (*github.Hook, bool, error) {
	log := log.FromContext(ctx)
	git, err := m.getClient(ctx)
	if err != nil {
		log.Error(err, "unable to create github client")
		return nil, false, err
	}

	opt := &github.ListOptions{
		PerPage: 100,
	}

	for {
		hooks, response, err := git.Repositories.ListHooks(ctx, m.gitWebhook.Spec.RepositoryOwner, m.gitWebhook.Spec.RepositoryName, opt)
		if err != nil && !IsNotFound(response) {
			log.Error(err, "unable to list hooks", "for repo", m.gitWebhook.Spec.RepositoryOwner+"/"+m.gitWebhook.Spec.RepositoryName)
			return nil, false, err
		}
		for _, hook := range hooks {
			if hook.Config["url"] == m.gitWebhook.Spec.WebhookURL {
				//found
				return hook, true, nil
			}
		}
		if response.NextPage == 0 {
			break
		}
		opt.Page = response.NextPage
	}
	return nil, false, nil
}

func IsNotFound(response *github.Response) bool {
	return response.Response.StatusCode == 404
}

func (m *GitHubWebHook) isEquivalent(ctx context.Context) (bool, error) {
	log := log.FromContext(ctx)
	desiredHook, err := m.toWebhook(ctx)
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
	actualHook.URL = nil
	actualHook.Type = nil
	actualHook.LastResponse = nil
	actualHook.PingURL = nil
	actualHook.TestURL = nil

	delete(actualHook.Config, "secret")
	delete(desiredHook.Config, "secret")

	return reflect.DeepEqual(desiredHook, actualHook), nil

}

func (m *GitHubWebHook) reconcile(ctx context.Context) error {
	log := log.FromContext(ctx)
	equivalent, err := m.isEquivalent(ctx)
	if err != nil {
		log.Error(err, "unable to determine if desired state is equal to actual state")
		return err
	}
	if equivalent {
		return nil
	}
	return m.createOrUpdateWebhook(ctx)
}

func (m *GitHubWebHook) createOrUpdateWebhook(ctx context.Context) error {
	log := log.FromContext(ctx)
	actualHook, found, err := m.getHook(ctx)
	if err != nil {
		log.Error(err, "error while retrieving webhook")
		return err
	}
	git, err := m.getClient(ctx)
	if err != nil {
		log.Error(err, "error get github client")
		return err
	}
	newHook, err := m.toWebhook(ctx)
	if err != nil {
		log.Error(err, "error to convert to github hook")
		return err
	}
	if !found {
		//we need to create
		_, _, err := git.Repositories.CreateHook(ctx, m.gitWebhook.Spec.RepositoryOwner, m.gitWebhook.Spec.RepositoryName, newHook)
		if err != nil {
			log.Error(err, "unable to create new hook")
			return err
		}
	} else {
		//we need to update
		_, _, err = git.Repositories.EditHook(ctx, m.gitWebhook.Spec.RepositoryOwner, m.gitWebhook.Spec.RepositoryName, *actualHook.ID, newHook)
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
	git, err := m.getClient(ctx)
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
	return m.reconcile(ctx)
}

func (m *GitHubWebHook) Delete(ctx context.Context) error {
	return m.deleteIfExists(ctx)
}
