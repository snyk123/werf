package helm

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/werf/werf/cmd/werf/common"
	"github.com/werf/werf/pkg/config"
	"github.com/werf/werf/pkg/git_repo"
	"github.com/werf/werf/pkg/werf"
)

var getReleaseCmdData common.CmdData

func NewGetReleaseCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:                   "get-release",
		DisableFlagsInUseLine: true,
		Short:                 "Print Helm Release name that will be used in current configuration with specified params",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := common.ProcessLogOptions(&getReleaseCmdData); err != nil {
				common.PrintHelp(cmd)
				return err
			}

			return runGetRelease()
		},
	}

	common.SetupDir(&getReleaseCmdData, cmd)
	common.SetupDisableDeterminism(&commonCmdData, cmd)
	common.SetupConfigPath(&getReleaseCmdData, cmd)
	common.SetupConfigTemplatesDir(&getReleaseCmdData, cmd)
	common.SetupTmpDir(&getReleaseCmdData, cmd)
	common.SetupHomeDir(&getReleaseCmdData, cmd)
	common.SetupEnvironment(&getReleaseCmdData, cmd)
	common.SetupDockerConfig(&getReleaseCmdData, cmd, "")

	common.SetupLogOptions(&getReleaseCmdData, cmd)

	return cmd
}

func runGetRelease() error {
	if err := werf.Init(*getReleaseCmdData.TmpDir, *getReleaseCmdData.HomeDir); err != nil {
		return fmt.Errorf("initialization error: %s", err)
	}

	projectDir, err := common.GetProjectDir(&getReleaseCmdData)
	if err != nil {
		return fmt.Errorf("getting project dir failed: %s", err)
	}

	localGitRepo, err := git_repo.OpenLocalRepo("own", projectDir)
	if err != nil {
		return fmt.Errorf("unable to open local repo %s: %s", projectDir, err)
	}

	werfConfig, err := common.GetRequiredWerfConfig(common.BackgroundContext(), projectDir, &getReleaseCmdData, localGitRepo, config.WerfConfigOptions{DisableDeterminism: *commonCmdData.DisableDeterminism})
	if err != nil {
		return fmt.Errorf("unable to load werf config: %s", err)
	}

	release, err := common.GetHelmRelease("", *getReleaseCmdData.Environment, werfConfig)
	if err != nil {
		return err
	}

	fmt.Println(release)

	return nil
}
