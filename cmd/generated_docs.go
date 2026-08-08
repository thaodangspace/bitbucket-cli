package cmd

import (
	"fmt"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// GenerateCommandMarkdown returns the command reference generated from the
// Cobra tree. Keeping this in the command package prevents the docs from
// silently drifting when a command or flag is added.
func GenerateCommandMarkdown() string {
	var b strings.Builder
	b.WriteString("---\ntitle: Generated command reference\ndescription: Generated from the Cobra command tree.\n---\n\n")
	var walk func(*cobra.Command, string)
	walk = func(cmd *cobra.Command, prefix string) {
		children := append([]*cobra.Command(nil), cmd.Commands()...)
		sort.Slice(children, func(i, j int) bool { return children[i].Name() < children[j].Name() })
		for _, child := range children {
			if !child.IsAvailableCommand() {
				continue
			}
			use := strings.TrimSpace(child.Use)
			if use == "" {
				use = child.Name()
			}
			fullName := strings.TrimSpace(prefix + use)
			b.WriteString(fmt.Sprintf("## `%s`\n\n%s\n\n", fullName, strings.TrimSpace(child.Short)))
			classification, scope := commandMetadata(fullName)
			b.WriteString(fmt.Sprintf("- Classification: **%s**\n- Required scopes: `%s`\n- Example: `%s`\n", classification, scope, "bitbucket-cli "+fullName))
			if strings.HasPrefix(fullName, "report upsert") || strings.HasPrefix(fullName, "annotation upsert") {
				b.WriteString("\n> Warning: report payloads and annotations are visible to repository users with access; never include secrets.\n")
			}
			if strings.HasPrefix(fullName, "commit diff") {
				b.WriteString("\nRange semantics: `A..B` follows Bitbucket's API semantics—commits reachable from B excluding commits reachable from A.\n")
			}
			if fields := jsonFieldsForCommand(fullName); fields != "" {
				b.WriteString("- JSON fields: `" + fields + "`\n")
			}
			b.WriteString("\n")
			flags := child.LocalNonPersistentFlags()
			var names []string
			flags.VisitAll(func(f *pflag.Flag) { names = append(names, fmt.Sprintf("`--%s` — %s", f.Name, f.Usage)) })
			sort.Strings(names)
			if len(names) > 0 {
				b.WriteString("Flags: " + strings.Join(names, "; ") + "\n\n")
			}
			walk(child, prefix+child.Name()+" ")
		}
	}
	walk(rootCmd, "")
	return strings.TrimRight(b.String(), "\n") + "\n"
}

func jsonFieldsForCommand(name string) string {
	switch {
	case strings.HasPrefix(name, "pr list"), strings.HasPrefix(name, "pr get"), strings.HasPrefix(name, "pr view"), strings.HasPrefix(name, "pr status"), strings.HasPrefix(name, "pr current"):
		return "id,title,state,author,source,destination,reviewers"
	case strings.HasPrefix(name, "branch list"), strings.HasPrefix(name, "branch view"), strings.HasPrefix(name, "branch get"):
		return "name,target,links,requested_target,resolved_target"
	case strings.HasPrefix(name, "tag list"), strings.HasPrefix(name, "tag view"), strings.HasPrefix(name, "tag get"):
		return "name,target,message,links,requested_target,resolved_target"
	case strings.HasPrefix(name, "branch-restriction"):
		return "id,kind,branch_match_kind,pattern,branch_type,value,users,groups"
	case strings.HasPrefix(name, "default-reviewer"):
		return "uuid,account_id,display_name,nickname,links"
	case strings.HasPrefix(name, "branching-model"):
		return "development,production,branch_types,default_branch_deletion"
	case strings.HasPrefix(name, "pipeline list"), strings.HasPrefix(name, "pipeline get"), strings.HasPrefix(name, "pipeline watch"):
		return "uuid,build_number,state,target,trigger,steps"
	case strings.HasPrefix(name, "pr comments"), strings.HasPrefix(name, "pr comment"):
		return "id,content,user,parent,inline,pending,resolution,created_on,updated_on"
	case strings.HasPrefix(name, "pr task list"):
		return "id,content,state,comment,creator,pending,resolved_on,resolved_by,created_on,updated_on"
	case strings.HasPrefix(name, "commit list"), strings.HasPrefix(name, "commit view"):
		return "hash,message,author,date,links,requested_selector,resolved_hash"
	case strings.HasPrefix(name, "commit comment"):
		return "id,content,user,inline,created_on,updated_on"
	case strings.HasPrefix(name, "commit status"):
		return "key,state,name,description,url,refname,created_on,updated_on"
	case strings.HasPrefix(name, "report"):
		return "uuid,title,details,result,reporter,report_type,data,created_on,updated_on"
	case strings.HasPrefix(name, "annotation"):
		return "external_id,path,file_path,line,start_line,end_line,summary,message,severity,result,link"
	case strings.HasPrefix(name, "repo list"), strings.HasPrefix(name, "repo view"), strings.HasPrefix(name, "repo get"), strings.HasPrefix(name, "repo create"), strings.HasPrefix(name, "repo edit"), strings.HasPrefix(name, "repo fork"):
		return "uuid,full_name,name,is_private,mainbranch,links"
	case strings.HasPrefix(name, "workspace"):
		return "name,slug,uuid,links,members,projects"
	case strings.HasPrefix(name, "project"):
		return "key,name,uuid,description,is_private,links,repositories"
	case strings.HasPrefix(name, "permission"):
		return "permission,user,group,repository,before,after,changed"
	default:
		return ""
	}
}

func commandMetadata(name string) (classification, scope string) {
	classification, scope = "read", "read:repository:bitbucket"
	if strings.HasPrefix(name, "pr ") {
		scope = "read:pullrequest:bitbucket"
	}
	if strings.HasPrefix(name, "workspace") {
		scope = "read:workspace:bitbucket"
	}
	if strings.HasPrefix(name, "project") {
		scope = "read:project:bitbucket"
	}
	if strings.HasPrefix(name, "permission") {
		scope = "read:repository:bitbucket"
	}
	if strings.HasPrefix(name, "pipeline ") {
		scope = "read:pipeline:bitbucket"
	}
	if strings.HasPrefix(name, "pipeline variable ") {
		scope = "read:pipeline:bitbucket"
	}
	if strings.HasPrefix(name, "pipeline runner ") {
		scope = "read:runner:bitbucket"
	}
	if strings.HasPrefix(name, "commit status") {
		scope = "read:repository:bitbucket"
	}
	if strings.HasPrefix(name, "report") || strings.HasPrefix(name, "annotation") {
		scope = "read:repository:bitbucket"
	}
	if strings.HasPrefix(name, "branching-model edit") || strings.HasPrefix(name, "branch-restriction create") || strings.HasPrefix(name, "branch-restriction edit") || strings.HasPrefix(name, "branch-restriction delete") || strings.HasPrefix(name, "default-reviewer add") || strings.HasPrefix(name, "default-reviewer remove") || name == "repo create" || strings.HasPrefix(name, "repo create ") || name == "repo edit [<workspace/repo>]" || strings.HasPrefix(name, "repo edit ") {
		return "admin", "admin:repository:bitbucket"
	}
	if name == "repo delete [<workspace/repo>]" || strings.HasPrefix(name, "repo delete ") {
		return "delete", "delete:repository:bitbucket"
	}
	if name == "repo fork [<workspace/repo>]" || strings.HasPrefix(name, "repo fork ") {
		return "write", "read:repository:bitbucket, write:repository:bitbucket"
	}
	if name == "repo set-default [<workspace/repo>]" || strings.HasPrefix(name, "repo set-default ") {
		return "write", "none (local git configuration)"
	}
	for _, write := range []string{"pr checkout", "pr comment", "pr attach", "pr create", "pr update", "pr review", "pr unapprove", "pr remove-change-request", "pr thread", "pr task create", "pr task update", "pr task delete", "pr merge", "pr decline", "pr reopen", "commit comment create", "commit comment edit", "commit comment delete", "commit approve", "commit unapprove", "commit status set", "report upsert", "report delete", "annotation upsert", "annotation delete", "auth login", "auth logout", "alias set", "alias delete", "branch create", "branch delete", "tag create", "tag delete", "branching-model edit", "branch-restriction create", "branch-restriction edit", "branch-restriction delete", "default-reviewer add", "default-reviewer remove", "pipeline run", "pipeline stop", "pipeline schedule create", "pipeline schedule edit", "pipeline schedule delete", "pipeline variable set", "pipeline variable delete", "pipeline cache delete", "pipeline runner create", "pipeline runner edit", "pipeline runner delete", "pipeline config enable", "pipeline config disable", "project create", "project edit", "project delete", "permission grant", "permission revoke", "workspace invite", "workspace remove-member"} {
		if name == write || strings.HasPrefix(name, write+" ") {
			if strings.HasPrefix(name, "pr ") {
				return "write", "read:pullrequest:bitbucket, write:pullrequest:bitbucket"
			}
			if strings.HasPrefix(name, "commit status") {
				return "write", "read:repository:bitbucket and write:repository:bitbucket"
			}
			if strings.HasPrefix(name, "commit comment") || strings.HasPrefix(name, "commit approve") || strings.HasPrefix(name, "commit unapprove") {
				return "write", "read:repository:bitbucket and write:repository:bitbucket"
			}
			if strings.HasPrefix(name, "report") || strings.HasPrefix(name, "annotation") {
				return "write", "read:repository:bitbucket and write:repository:bitbucket"
			}
			if strings.HasPrefix(name, "pipeline variable ") {
				return "write", "admin:pipeline:bitbucket"
			}
			if strings.HasPrefix(name, "pipeline runner create") || strings.HasPrefix(name, "pipeline runner edit") {
				return "write", "read:runner:bitbucket and write:runner:bitbucket"
			}
			if strings.HasPrefix(name, "pipeline runner delete") {
				return "write", "write:runner:bitbucket"
			}
			if strings.HasPrefix(name, "pipeline ") {
				return "write", "write:pipeline:bitbucket"
			}
			if strings.HasPrefix(name, "project ") {
				return "write", "admin:project:bitbucket"
			}
			if strings.HasPrefix(name, "permission grant") {
				return "write", "admin:repository:bitbucket, write:permission:bitbucket"
			}
			if strings.HasPrefix(name, "permission revoke") {
				return "write", "admin:repository:bitbucket, delete:permission:bitbucket"
			}
			if strings.HasPrefix(name, "workspace ") {
				return "write", "admin:workspace:bitbucket"
			}
			return "write", scope
		}
	}
	return classification, scope
}
