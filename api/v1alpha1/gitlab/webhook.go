package gitlab

import (
	"context"
	"errors"
	"reflect"

	redhatcopv1alpha1 "github.com/redhat-cop/gitwebhook-operator/api/v1alpha1"
	"github.com/xanzy/go-gitlab"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

type GitLabWebHook struct {
	gitWebhook *redhatcopv1alpha1.GitWebhook
	project    *gitlab.Project
	webhook    *gitlab.ProjectHook
}

var _ redhatcopv1alpha1.WebHook = &GitLabWebHook{}

func FromGitWebhook(gitwebhook *redhatcopv1alpha1.GitWebhook) *GitLabWebHook {
	return &GitLabWebHook{
		gitWebhook: gitwebhook,
	}
}

func (m *GitLabWebHook) Reconcile(ctx context.Context) error {
	return m.reconcile(ctx)
}

func (m *GitLabWebHook) Delete(ctx context.Context) error {
	return m.deleteIfExists(ctx)
}

func (m *GitLabWebHook) deleteIfExists(ctx context.Context) error {
	log := log.FromContext(ctx)
	project, found, err := m.getProject(ctx)
	if err != nil {
		log.Error(err, "unable to get gitlab project")
		return err
	}
	if !found {
		return errors.New("unable to find project")
	}
	hook, found, err := m.getHook(ctx)
	if err != nil {
		log.Error(err, "unable to retrieve webhook")
		return err
	}
	if !found {
		return nil
	}
	git, err := m.getGitLabClient(ctx)
	if err != nil {
		log.Error(err, "unable to create gitlab client")
		return err
	}
	_, err = git.Projects.DeleteProjectHook(project.ID, hook.ID, gitlab.WithContext(ctx))
	if err != nil {
		log.Error(err, "unable to delete webhook")
		return err
	}
	return nil
}

func (m *GitLabWebHook) reconcile(ctx context.Context) error {
	log := log.FromContext(ctx)
	equivalent, err := m.isEquivalent(ctx)
	if err != nil {
		log.Error(err, "unable determine equivalency with actual state")
		return err
	}
	if !equivalent {
		return m.createOrUpdate(ctx)
	}
	return nil
}

func (m *GitLabWebHook) createOrUpdate(ctx context.Context) error {
	log := log.FromContext(ctx)
	project, found, err := m.getProject(ctx)
	if err != nil {
		log.Error(err, "unable to get gitlab project")
		return err
	}
	if !found {
		return errors.New("unable to find project")
	}
	actualHook, found, err := m.getHook(ctx)
	if err != nil {
		log.Error(err, "unable to retrieve webhook")
		return err
	}
	git, err := m.getGitLabClient(ctx)
	if err != nil {
		log.Error(err, "unable to create gitlab client")
		return err
	}
	if !found {
		//we need to create it
		hook, err := m.toAddProjectHookOptions(ctx)
		if err != nil {
			log.Error(err, "unable to convert to ProjectHookOptions")
			return err
		}
		_, _, err = git.Projects.AddProjectHook(project.ID, hook, gitlab.WithContext(ctx))
		if err != nil {
			log.Error(err, "unable to create webhook")
			return err
		}
	} else {
		//we need to update it
		hook, err := m.toEditProjectHookOptions(ctx)
		if err != nil {
			log.Error(err, "unable to convert to ProjectHookOptions")
			return err
		}
		_, _, err = git.Projects.EditProjectHook(project.ID, actualHook.ID, hook, gitlab.WithContext(ctx))
		if err != nil {
			log.Error(err, "unable to update webhook")
			return err
		}
	}
	return nil
}

func (m *GitLabWebHook) isEquivalent(ctx context.Context) (bool, error) {
	log := log.FromContext(ctx)
	desiredHook, err := m.toProjectHook(ctx)
	if err != nil {
		log.Error(err, "unable convert to gitlab webhook")
		return false, err
	}
	actualHook, found, err := m.getHook(ctx)
	if err != nil {
		log.Error(err, "error while retrieving gitlab webhook")
		return false, err
	}
	if !found {
		return false, nil
	}
	actualHook.CreatedAt = nil
	actualHook.ID = 0
	actualHook.ProjectID = 0
	return reflect.DeepEqual(desiredHook, actualHook), nil
}

func (m *GitLabWebHook) getProject(ctx context.Context) (*gitlab.Project, bool, error) {
	if m.project != nil {
		return m.project, true, nil
	}
	log := log.FromContext(ctx)
	if m.project != nil {
		return m.project, true, nil
	}
	git, err := m.getGitLabClient(ctx)
	if err != nil {
		log.Error(err, "unable to create gitlab client")
		return nil, false, err
	}
	projects, _, err := git.Projects.ListUserProjects(m.gitWebhook.Spec.RepositoryOwner, &gitlab.ListProjectsOptions{}, gitlab.WithContext(ctx))
	if err != nil {
		log.Error(err, "unable to list gitlab projects", "for owner", m.gitWebhook.Spec.RepositoryOwner)
		return nil, false, err
	}
	for _, project := range projects {
		if project.Name == m.gitWebhook.Spec.RepositoryName {
			m.project = project
			return project, true, nil
		}
	}
	// if we get here we need to try the group projects
	projects, _, err = git.Groups.ListGroupProjects(m.gitWebhook.Spec.RepositoryOwner, &gitlab.ListGroupProjectsOptions{}, gitlab.WithContext(ctx))
	if err != nil {
		log.Error(err, "unable to list gitlab projects", "for owner", m.gitWebhook.Spec.RepositoryOwner)
		return nil, false, err
	}
	for _, project := range projects {
		if project.Name == m.gitWebhook.Spec.RepositoryName {
			m.project = project
			return project, true, nil
		}
	}
	return nil, false, nil
}

func (m *GitLabWebHook) getHook(ctx context.Context) (*gitlab.ProjectHook, bool, error) {
	if m.webhook != nil {
		return m.webhook, true, nil
	}
	log := log.FromContext(ctx)
	git, err := m.getGitLabClient(ctx)
	if err != nil {
		log.Error(err, "unable to create gitlab client")
		return nil, false, err
	}
	project, found, err := m.getProject(ctx)
	if err != nil {
		log.Error(err, "unable to get gitlab project")
		return nil, false, err
	}
	if !found {
		return nil, false, nil
	}
	hooks, _, err := git.Projects.ListProjectHooks(project.ID, &gitlab.ListProjectHooksOptions{}, gitlab.WithContext(ctx))
	if err != nil {
		log.Error(err, "unable to retrieve hooks for project")
		return nil, false, err
	}
	for _, hook := range hooks {
		if hook.URL == m.gitWebhook.Spec.WebhookURL {
			m.webhook = hook
			return hook, true, nil
		}
	}
	return nil, false, nil
}

func (m *GitLabWebHook) toEditProjectHookOptions(ctx context.Context) (*gitlab.EditProjectHookOptions, error) {
	log := log.FromContext(ctx)
	secret, err := m.gitWebhook.GetWebhookSecret(ctx)
	if err != nil {
		log.Error(err, "unable to retrieve webhook secret")
		return nil, err
	}
	editProjectHookOptions := gitlab.EditProjectHookOptions{
		EnableSSLVerification:  &m.gitWebhook.Spec.InsecureSSL,
		Token:                  &secret,
		URL:                    &m.gitWebhook.Spec.WebhookURL,
		PushEventsBranchFilter: &m.gitWebhook.Spec.PushEventBranchFilter,
	}
	err = m.addGitLabEventsToEditProjectHookOptions(&editProjectHookOptions)
	if err != nil {
		return nil, err
	}
	return &editProjectHookOptions, nil
}

func (m *GitLabWebHook) toProjectHook(ctx context.Context) (*gitlab.ProjectHook, error) {
	projectHook := gitlab.ProjectHook{
		EnableSSLVerification:  m.gitWebhook.Spec.InsecureSSL,
		URL:                    m.gitWebhook.Spec.WebhookURL,
		PushEventsBranchFilter: m.gitWebhook.Spec.PushEventBranchFilter,
	}
	err := m.addGitLabEventsToProjectHook(&projectHook)
	if err != nil {
		return nil, err
	}
	return &projectHook, nil
}

func (m *GitLabWebHook) toAddProjectHookOptions(ctx context.Context) (*gitlab.AddProjectHookOptions, error) {
	log := log.FromContext(ctx)
	secret, err := m.gitWebhook.GetWebhookSecret(ctx)
	if err != nil {
		log.Error(err, "unable to retrieve webhook secret")
		return nil, err
	}
	addProjectOptions := gitlab.AddProjectHookOptions{
		EnableSSLVerification:  &m.gitWebhook.Spec.InsecureSSL,
		Token:                  &secret,
		URL:                    &m.gitWebhook.Spec.WebhookURL,
		PushEventsBranchFilter: &m.gitWebhook.Spec.PushEventBranchFilter,
	}
	err = m.addGitLabEventsToAddProjectHookOptions(&addProjectOptions)
	if err != nil {
		return nil, err
	}
	return &addProjectOptions, nil
}

func (m *GitLabWebHook) addGitLabEventsToProjectHook(projectHook *gitlab.ProjectHook) error {
	True := true
	for _, event := range m.gitWebhook.Spec.Events {
		switch event {
		case "confidential_issues_events":
			projectHook.ConfidentialIssuesEvents = True
		case "confidential_note_events":
			projectHook.ConfidentialNoteEvents = True
		case "deployment_events":
			projectHook.DeploymentEvents = True
		case "issues_events":
			projectHook.IssuesEvents = True
		case "job_events":
			projectHook.JobEvents = True
		case "merge_requests_events":
			projectHook.MergeRequestsEvents = True
		case "note_events":
			projectHook.NoteEvents = True
		case "pipeline_events":
			projectHook.PipelineEvents = True
		case "push_events":
			projectHook.PushEvents = True
		case "ReleasesEvents":
			projectHook.ReleasesEvents = True
		case "tag_push_events":
			projectHook.TagPushEvents = True
		case "wiki_page_events":
			projectHook.WikiPageEvents = True
		default:
			return errors.New("unknown event type:" + event)
		}
	}
	return nil
}

func (m *GitLabWebHook) addGitLabEventsToAddProjectHookOptions(addProjectHookOptions *gitlab.AddProjectHookOptions) error {
	True := true
	for _, event := range m.gitWebhook.Spec.Events {
		switch event {
		case "confidential_issues_events":
			addProjectHookOptions.ConfidentialIssuesEvents = &True
		case "confidential_note_events":
			addProjectHookOptions.ConfidentialNoteEvents = &True
		case "deployment_events":
			addProjectHookOptions.DeploymentEvents = &True
		case "issues_events":
			addProjectHookOptions.IssuesEvents = &True
		case "job_events":
			addProjectHookOptions.JobEvents = &True
		case "merge_requests_events":
			addProjectHookOptions.MergeRequestsEvents = &True
		case "note_events":
			addProjectHookOptions.NoteEvents = &True
		case "pipeline_events":
			addProjectHookOptions.PipelineEvents = &True
		case "push_events":
			addProjectHookOptions.PushEvents = &True
		case "ReleasesEvents":
			addProjectHookOptions.ReleasesEvents = &True
		case "tag_push_events":
			addProjectHookOptions.TagPushEvents = &True
		case "wiki_page_events":
			addProjectHookOptions.WikiPageEvents = &True
		default:
			return errors.New("unknown event type:" + event)
		}
	}
	return nil
}

func (m *GitLabWebHook) addGitLabEventsToEditProjectHookOptions(editProjectHookOptions *gitlab.EditProjectHookOptions) error {
	True := true
	for _, event := range m.gitWebhook.Spec.Events {
		switch event {
		case "confidential_issues_events":
			editProjectHookOptions.ConfidentialIssuesEvents = &True
		case "confidential_note_events":
			editProjectHookOptions.ConfidentialNoteEvents = &True
		case "deployment_events":
			editProjectHookOptions.DeploymentEvents = &True
		case "issues_events":
			editProjectHookOptions.IssuesEvents = &True
		case "job_events":
			editProjectHookOptions.JobEvents = &True
		case "merge_requests_events":
			editProjectHookOptions.MergeRequestsEvents = &True
		case "note_events":
			editProjectHookOptions.NoteEvents = &True
		case "pipeline_events":
			editProjectHookOptions.PipelineEvents = &True
		case "push_events":
			editProjectHookOptions.PushEvents = &True
		case "ReleasesEvents":
			editProjectHookOptions.ReleasesEvents = &True
		case "tag_push_events":
			editProjectHookOptions.TagPushEvents = &True
		case "wiki_page_events":
			editProjectHookOptions.WikiPageEvents = &True
		default:
			return errors.New("unknown event type:" + event)
		}
	}
	return nil
}

func (m *GitLabWebHook) getGitLabClient(ctx context.Context) (*gitlab.Client, error) {
	log := log.FromContext(ctx)
	token, err := m.gitWebhook.GetGitCredential(ctx, m.gitWebhook.Spec.GitLab)
	if err != nil {
		log.Error(err, "Unable to retrieve gitlab credential", "secret", m.gitWebhook.Spec.GitLab.GitServerCredentials.Name)
		return nil, err
	}
	var git *gitlab.Client
	if m.gitWebhook.Spec.GitLab.GitAPIServerURL != "" {
		git, err = gitlab.NewClient(token, gitlab.WithBaseURL(m.gitWebhook.Spec.GitLab.GitAPIServerURL))
		if err != nil {
			log.Error(err, "Failed to create gitlab client", "url", m.gitWebhook.Spec.GitLab.GitAPIServerURL)
			return nil, err
		}
	} else {
		git, err = gitlab.NewClient(token)
		if err != nil {
			log.Error(err, "Failed to create gitlab client")
			return nil, err
		}
	}
	return git, nil
}
