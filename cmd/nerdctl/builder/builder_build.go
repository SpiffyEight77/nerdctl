/*
   Copyright The containerd Authors.

   Licensed under the Apache License, Version 2.0 (the "License");
   you may not use this file except in compliance with the License.
   You may obtain a copy of the License at

       http://www.apache.org/licenses/LICENSE-2.0

   Unless required by applicable law or agreed to in writing, software
   distributed under the License is distributed on an "AS IS" BASIS,
   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
   See the License for the specific language governing permissions and
   limitations under the License.
*/

package builder

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/containerd/log"

	"github.com/containerd/nerdctl/v2/cmd/nerdctl/completion"
	"github.com/containerd/nerdctl/v2/cmd/nerdctl/helpers"
	"github.com/containerd/nerdctl/v2/pkg/api/types"
	"github.com/containerd/nerdctl/v2/pkg/buildkitutil"
	"github.com/containerd/nerdctl/v2/pkg/clientutil"
	"github.com/containerd/nerdctl/v2/pkg/cmd/builder"
	"github.com/containerd/nerdctl/v2/pkg/strutil"
)

func BuildCommand() *cobra.Command {
	var cmd = &cobra.Command{
		Use:   "build [flags] PATH",
		Short: "Build an image from a Dockerfile. Needs buildkitd to be running.",
		Long: `Build an image from a Dockerfile. Needs buildkitd to be running.
If Dockerfile is not present and -f is not specified, it will look for Containerfile and build with it. `,
		RunE:          buildAction,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	cmd.Flags().String("buildkit-host", "", "BuildKit address")
	cmd.Flags().StringArray("add-host", nil, "Add a custom host-to-IP mapping (format: \"host:ip\")")
	cmd.Flags().StringArrayP("tag", "t", nil, "Name and optionally a tag in the 'name:tag' format")
	cmd.Flags().StringP("file", "f", "", "Name of the Dockerfile")
	cmd.Flags().String("target", "", "Set the target build stage to build")
	cmd.Flags().StringArray("build-arg", nil, "Set build-time variables")
	cmd.Flags().Bool("no-cache", false, "Do not use cache when building the image")
	cmd.Flags().StringP("output", "o", "", "Output destination (format: type=local,dest=path)")
	cmd.Flags().String("progress", "auto", "Set type of progress output (auto, plain, tty). Use plain to show container output")
	cmd.Flags().String("provenance", "", "Shorthand for \"--attest=type=provenance\"")
	cmd.Flags().Bool("pull", false, "On true, always attempt to pull latest image version from remote. Default uses buildkit's default.")
	cmd.Flags().StringArray("secret", nil, "Secret file to expose to the build: id=mysecret,src=/local/secret")
	cmd.Flags().StringArray("allow", nil, "Allow extra privileged entitlement, e.g. network.host, security.insecure")
	cmd.RegisterFlagCompletionFunc("allow", func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		return []string{"network.host", "security.insecure"}, cobra.ShellCompDirectiveNoFileComp
	})
	cmd.Flags().StringArray("attest", nil, "Attestation parameters (format: \"type=sbom,generator=image\")")
	cmd.Flags().StringArray("ssh", nil, "SSH agent socket or keys to expose to the build (format: default|<id>[=<socket>|<key>[,<key>]])")
	cmd.Flags().BoolP("quiet", "q", false, "Suppress the build output and print image ID on success")
	cmd.Flags().String("sbom", "", "Shorthand for \"--attest=type=sbom\"")
	cmd.Flags().StringArray("cache-from", nil, "External cache sources (eg. user/app:cache, type=local,src=path/to/dir)")
	cmd.Flags().StringArray("cache-to", nil, "Cache export destinations (eg. user/app:cache, type=local,dest=path/to/dir)")
	cmd.Flags().Bool("rm", true, "Remove intermediate containers after a successful build")
	cmd.Flags().String("network", "default", "Set type of network for build (format:network=default|none|host)")
	cmd.RegisterFlagCompletionFunc("network", func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		return []string{"default", "host", "none"}, cobra.ShellCompDirectiveNoFileComp
	})
	// #region platform flags
	// platform is defined as StringSlice, not StringArray, to allow specifying "--platform=amd64,arm64"
	cmd.Flags().StringSlice("platform", []string{}, "Set target platform for build (e.g., \"amd64\", \"arm64\")")
	cmd.RegisterFlagCompletionFunc("platform", completion.Platforms)
	cmd.Flags().StringArray("build-context", []string{}, "Additional build contexts (e.g., name=path)")
	// #endregion

	cmd.Flags().String("iidfile", "", "Write the image ID to the file")
	cmd.Flags().StringArray("label", nil, "Set metadata for an image")

	return cmd
}

func processBuildCommandFlag(cmd *cobra.Command, args []string) (types.BuilderBuildOptions, error) {
	globalOptions, err := helpers.ProcessRootCmdFlags(cmd)
	if err != nil {
		return types.BuilderBuildOptions{}, err
	}
	buildKitHost, err := GetBuildkitHost(cmd, globalOptions.Namespace)
	if err != nil {
		return types.BuilderBuildOptions{}, err
	}
	extraHosts, err := cmd.Flags().GetStringArray("add-host")
	if err != nil {
		return types.BuilderBuildOptions{}, err
	}
	platform, err := cmd.Flags().GetStringSlice("platform")
	if err != nil {
		return types.BuilderBuildOptions{}, err
	}
	platform = strutil.DedupeStrSlice(platform)
	if len(args) < 1 {
		return types.BuilderBuildOptions{}, errors.New("context needs to be specified")
	}
	buildContext := args[0]
	if buildContext == "-" || strings.Contains(buildContext, "://") {
		return types.BuilderBuildOptions{}, fmt.Errorf("unsupported build context: %q", buildContext)
	}
	output, err := cmd.Flags().GetString("output")
	if err != nil {
		return types.BuilderBuildOptions{}, err
	}
	tagValue, err := cmd.Flags().GetStringArray("tag")
	if err != nil {
		return types.BuilderBuildOptions{}, err
	}
	progress, err := cmd.Flags().GetString("progress")
	if err != nil {
		return types.BuilderBuildOptions{}, err
	}
	filename, err := cmd.Flags().GetString("file")
	if err != nil {
		return types.BuilderBuildOptions{}, err
	}
	target, err := cmd.Flags().GetString("target")
	if err != nil {
		return types.BuilderBuildOptions{}, err
	}
	buildArgs, err := cmd.Flags().GetStringArray("build-arg")
	if err != nil {
		return types.BuilderBuildOptions{}, err
	}
	label, err := cmd.Flags().GetStringArray("label")
	if err != nil {
		return types.BuilderBuildOptions{}, err
	}
	noCache, err := cmd.Flags().GetBool("no-cache")
	if err != nil {
		return types.BuilderBuildOptions{}, err
	}
	var pull *bool
	if cmd.Flags().Changed("pull") {
		pullFlag, err := cmd.Flags().GetBool("pull")
		if err != nil {
			return types.BuilderBuildOptions{}, err
		}
		pull = &pullFlag
	}
	secret, err := cmd.Flags().GetStringArray("secret")
	if err != nil {
		return types.BuilderBuildOptions{}, err
	}
	allow, err := cmd.Flags().GetStringArray("allow")
	if err != nil {
		return types.BuilderBuildOptions{}, err
	}
	ssh, err := cmd.Flags().GetStringArray("ssh")
	if err != nil {
		return types.BuilderBuildOptions{}, err
	}
	cacheFrom, err := cmd.Flags().GetStringArray("cache-from")
	if err != nil {
		return types.BuilderBuildOptions{}, err
	}
	cacheTo, err := cmd.Flags().GetStringArray("cache-to")
	if err != nil {
		return types.BuilderBuildOptions{}, err
	}
	rm, err := cmd.Flags().GetBool("rm")
	if err != nil {
		return types.BuilderBuildOptions{}, err
	}
	iidfile, err := cmd.Flags().GetString("iidfile")
	if err != nil {
		return types.BuilderBuildOptions{}, err
	}
	quiet, err := cmd.Flags().GetBool("quiet")
	if err != nil {
		return types.BuilderBuildOptions{}, err
	}
	network, err := cmd.Flags().GetString("network")
	if err != nil {
		return types.BuilderBuildOptions{}, err
	}

	attest, err := cmd.Flags().GetStringArray("attest")
	if err != nil {
		return types.BuilderBuildOptions{}, err
	}
	sbom, err := cmd.Flags().GetString("sbom")
	if err != nil {
		return types.BuilderBuildOptions{}, err
	}
	if sbom != "" {
		attest = append(attest, canonicalizeAttest("sbom", sbom))
	}
	provenance, err := cmd.Flags().GetString("provenance")
	if err != nil {
		return types.BuilderBuildOptions{}, err
	}
	if provenance != "" {
		attest = append(attest, canonicalizeAttest("provenance", provenance))
	}
	extendedBuildCtx, err := cmd.Flags().GetStringArray("build-context")
	if err != nil {
		return types.BuilderBuildOptions{}, err
	}

	usernsRemap, err := cmd.Flags().GetString("userns-remap")
	if err != nil {
		return types.BuilderBuildOptions{}, err
	} else if usernsRemap != "" {
		log.L.Warn("userns remap is not supported with nerdctl build. dropping the config.")
	}

	return types.BuilderBuildOptions{
		GOptions:             globalOptions,
		BuildKitHost:         buildKitHost,
		BuildContext:         buildContext,
		Output:               output,
		Tag:                  tagValue,
		Progress:             progress,
		File:                 filename,
		Target:               target,
		BuildArgs:            buildArgs,
		Label:                label,
		NoCache:              noCache,
		Pull:                 pull,
		Secret:               secret,
		Allow:                allow,
		Attest:               attest,
		SSH:                  ssh,
		CacheFrom:            cacheFrom,
		CacheTo:              cacheTo,
		Rm:                   rm,
		IidFile:              iidfile,
		Quiet:                quiet,
		Platform:             platform,
		Stdout:               cmd.OutOrStdout(),
		Stderr:               cmd.OutOrStderr(),
		Stdin:                cmd.InOrStdin(),
		NetworkMode:          network,
		ExtendedBuildContext: extendedBuildCtx,
		ExtraHosts:           extraHosts,
	}, nil
}

func GetBuildkitHost(cmd *cobra.Command, namespace string) (string, error) {
	if cmd.Flags().Changed("buildkit-host") {
		// If address is explicitly specified, use it.
		buildkitHost, err := cmd.Flags().GetString("buildkit-host")
		if err != nil {
			return "", err
		}
		if err := buildkitutil.PingBKDaemon(buildkitHost); err != nil {
			return "", err
		}
		return buildkitHost, nil
	}

	return buildkitutil.GetBuildkitHost(namespace)
}

func buildAction(cmd *cobra.Command, args []string) error {
	options, err := processBuildCommandFlag(cmd, args)
	if err != nil {
		return err
	}

	client, ctx, cancel, err := clientutil.NewClient(cmd.Context(), options.GOptions.Namespace, options.GOptions.Address)
	if err != nil {
		return err
	}
	defer cancel()

	return builder.Build(ctx, client, options)
}

// canonicalizeAttest is from https://github.com/docker/buildx/blob/v0.12/util/buildflags/attests.go##L13-L21
func canonicalizeAttest(attestType string, in string) string {
	if in == "" {
		return ""
	}
	if b, err := strconv.ParseBool(in); err == nil {
		return fmt.Sprintf("type=%s,disabled=%t", attestType, !b)
	}
	return fmt.Sprintf("type=%s,%s", attestType, in)
}
