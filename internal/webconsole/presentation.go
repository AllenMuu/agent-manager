package webconsole

import "strings"

func messageRef(key string, args map[string]string) *messageRefDTO {
	if key == "" {
		return nil
	}
	return &messageRefDTO{Key: key, Args: args}
}

func operationMessageRef(operation string) *messageRefDTO {
	keys := map[string]string{
		"activate":                         "plan.operation.activate",
		"activate selected skills":         "plan.operation.activate_selected_skills",
		"resynchronize managed Skills":     "plan.operation.resynchronize_managed_skills",
		"remove":                           "plan.operation.remove",
		"reconcile":                        "plan.operation.reconcile",
		"update managed-link Git guidance": "plan.operation.gitignore",
		"undo":                             "plan.operation.undo",
	}
	if key, ok := keys[operation]; ok {
		return messageRef(key, nil)
	}
	if action, ok := strings.CutPrefix(operation, "undo "); ok {
		if key, ok := keys[action]; ok {
			return messageRef("plan.operation.undo_action", map[string]string{"operation": key})
		}
		return messageRef("plan.operation.undo", nil)
	}
	return nil
}

func actionMessageRef(action string) *messageRefDTO {
	key, ok := map[string]string{
		"create absolute link":                        "plan.action.create_link",
		"replace conflicting path with absolute link": "plan.action.replace_conflict",
		"remove managed link":                         "plan.action.remove_managed_link",
		"restore pre-operation state":                 "plan.action.restore_previous_state",
		"refuse conflicting destination":              "plan.action.refuse_conflict",
		"replace conflict (force required)":           "plan.action.replace_conflict_force_required",
		"copy project skill into library":             "plan.action.copy_skill_to_library",
		"replace directory with managed link":         "plan.action.replace_with_managed_link",
		"repoint orphaned managed link":               "plan.action.repoint_orphaned_link",
		"ignore managed link":                         "plan.action.gitignore_managed_link",
	}[action]
	if !ok {
		return nil
	}
	return messageRef(key, nil)
}

func warningMessageRef(warning string) *messageRefDTO {
	const compatibilitySuffix = " is not declared compatible with "
	if skill, target, ok := strings.Cut(warning, compatibilitySuffix); ok && skill != "" && target != "" {
		return messageRef("plan.warning.compatibility_not_declared", map[string]string{"skill": skill, "target": target})
	}
	if warning == "resource is already installed; no changes made" {
		return messageRef("plan.warning.already_installed", nil)
	}
	return nil
}
