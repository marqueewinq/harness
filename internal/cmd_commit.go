package internal

import "fmt"

func (a *App) CmdWorkCommit(taskID, message string, extraGitArgs []string) error {
	id, err := a.ResolveTaskID(taskID)
	if err != nil {
		return err
	}
	task, err := a.DB.GetTask(id)
	if err != nil {
		return err
	}

	// Resolve hierarchy: task -> epic -> project
	var epicID, projID string
	if task.EpicID != "" {
		epicID = task.EpicID
		if epic, err := a.DB.GetEpic(task.EpicID); err == nil {
			projID = epic.ProjectID
		}
	}

	rendered := BuildCommitMessage(a.Config.Commit.Message, task.ID, epicID, projID, message)

	wtGit := NewGitClient(task.WorktreePath)
	if err := wtGit.Commit(rendered, extraGitArgs...); err != nil {
		return fmt.Errorf("git commit: %w", err)
	}

	fmt.Printf("Committed: %s\n", rendered)
	return nil
}
