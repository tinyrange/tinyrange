package cli

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/tinyrange/tinyrange/pkg/builder"
	"github.com/tinyrange/tinyrange/pkg/common"
	"github.com/tinyrange/tinyrange/pkg/config"
	"github.com/tinyrange/tinyrange/pkg/login"
)

var ociCmd = &cobra.Command{
	Use:   "oci",
	Short: "Docker compatible OCI shim",
}

var ociRunCmd = &cobra.Command{
	Use:   "run [OPTIONS] IMAGE [COMMAND] [ARG...]",
	Short: "Run an OCI container similar to docker run",
	RunE: func(cmd *cobra.Command, args []string) error {
		fs := pflag.NewFlagSet("oci-run", pflag.ContinueOnError)
		fs.ParseErrorsWhitelist.UnknownFlags = true

		var (
			rm          bool
			name        string
			publish     []string
			env         []string
			volume      []string
			detach      bool
			pullPolicy  string
			interactive bool
			tty         bool
		)

		fs.BoolVar(&rm, "rm", false, "Automatically remove the container when it exits")
		fs.BoolVarP(&interactive, "interactive", "i", false, "Keep STDIN open even if not attached")
		fs.BoolVarP(&tty, "tty", "t", false, "Allocate a pseudo-TTY")
		fs.StringVar(&name, "name", "", "Assign a name to the container")
		fs.StringArrayVarP(&publish, "publish", "p", []string{}, "Publish a container's port(s) to the host")
		fs.StringArrayVarP(&env, "env", "e", []string{}, "Set environment variables")
		fs.StringArrayVarP(&volume, "volume", "v", []string{}, "Bind mount a volume")
		fs.BoolVarP(&detach, "detach", "d", false, "Run container in background")
		fs.StringVar(&pullPolicy, "pull", "", "Set image pull policy")

		if err := fs.Parse(args); err != nil && !errors.Is(err, pflag.ErrHelp) {
			return err
		}

		rest := fs.Args()
		var unknown []string
		for len(rest) > 0 && strings.HasPrefix(rest[0], "-") {
			unknownFlag := rest[0]
			unknown = append(unknown, unknownFlag)
			rest = rest[1:]
			if len(rest) > 0 && !strings.HasPrefix(rest[0], "-") {
				rest = rest[1:]
			}
		}

		if len(rest) == 0 {
			return fmt.Errorf("please specify an image")
		}

		image := rest[0]
		cmdArgs := rest[1:]

		for _, u := range unknown {
			fmt.Fprintf(cmd.ErrOrStderr(), "warning: unsupported flag %s\n", u)
		}
		if rm {
			fmt.Fprintln(cmd.ErrOrStderr(), "warning: --rm is ignored; TinyRange does not keep containers")
		}
		if detach {
			fmt.Fprintln(cmd.ErrOrStderr(), "warning: --detach is not supported")
		}
		if pullPolicy != "" {
			fmt.Fprintln(cmd.ErrOrStderr(), "warning: --pull is not supported")
		}

		conf := login.Config{
			Version:      login.CURRENT_CONFIG_VERSION,
			OciImage:     image,
			Environment:  env,
			InstanceName: name,
			CpuCores:     1,
			MemorySize:   1024,
			StorageSize:  1024,
		}

		for _, p := range publish {
			listen := ""
			hostPort := ""
			containerPort := ""
			parts := strings.Split(p, ":")
			switch len(parts) {
			case 1:
				containerPort = parts[0]
			case 2:
				hostPort = parts[0]
				containerPort = parts[1]
			case 3:
				listen = parts[0]
				hostPort = parts[1]
				containerPort = parts[2]
			}
			if hostPort != "" && hostPort != containerPort {
				fmt.Fprintf(cmd.ErrOrStderr(), "warning: different host (%s) and container (%s) ports are not supported\n", hostPort, containerPort)
			}
			fp := containerPort
			if listen != "" {
				fp = listen + ":" + containerPort
			}
			conf.ForwardPorts = append(conf.ForwardPorts, fp)
		}

		for _, v := range volume {
			parts := strings.Split(v, ":")
			if len(parts) < 2 {
				fmt.Fprintf(cmd.ErrOrStderr(), "warning: could not parse volume %q\n", v)
				continue
			}
			host := parts[0]
			guest := parts[1]
			opts := ""
			if len(parts) > 2 {
				opts = parts[2]
			}
			mount := host + ":" + guest
			if strings.Contains(opts, "ro") {
				conf.ReadOnlyMounts = append(conf.ReadOnlyMounts, mount)
			} else {
				conf.ReadWriteMounts = append(conf.ReadWriteMounts, mount)
			}
		}

		if len(cmdArgs) > 0 {
			conf.Commands = []string{strings.Join(cmdArgs, " ")}
		}

		db, err := newDb()
		if err != nil {
			return err
		}
		conf.SetLocalConfig()
		wd, err := os.Getwd()
		if err != nil {
			return err
		}
		conf.SetBasePath(wd)

		return conf.Run(db)
	},
}

var ociPullCmd = &cobra.Command{
	Use:   "pull [OPTIONS] IMAGE",
	Short: "Pull an OCI container image",
	RunE: func(cmd *cobra.Command, args []string) error {
		fs := pflag.NewFlagSet("oci-pull", pflag.ContinueOnError)
		fs.ParseErrorsWhitelist.UnknownFlags = true

		var (
			platform            string
			allTags             bool
			quiet               bool
			disableContentTrust bool
			unpack              bool
		)

		fs.StringVar(&platform, "platform", "", "Set platform for image")
		fs.BoolVarP(&allTags, "all-tags", "a", false, "Download all tagged images in the repository")
		fs.BoolVar(&quiet, "quiet", false, "Suppress verbose output")
		fs.BoolVar(&disableContentTrust, "disable-content-trust", false, "Skip image verification")
		fs.BoolVar(&unpack, "unpack", true, "Unpack the image")

		if err := fs.Parse(args); err != nil && !errors.Is(err, pflag.ErrHelp) {
			return err
		}

		rest := fs.Args()
		var unknown []string
		for len(rest) > 0 && strings.HasPrefix(rest[0], "-") {
			unknownFlag := rest[0]
			unknown = append(unknown, unknownFlag)
			rest = rest[1:]
			if len(rest) > 0 && !strings.HasPrefix(rest[0], "-") {
				rest = rest[1:]
			}
		}

		if len(rest) == 0 {
			return fmt.Errorf("please specify an image to pull")
		}

		image := rest[0]

		for _, u := range unknown {
			fmt.Fprintf(cmd.ErrOrStderr(), "warning: unsupported flag %s\n", u)
		}
		if platform != "" {
			fmt.Fprintln(cmd.ErrOrStderr(), "warning: --platform is not supported")
		}
		if allTags {
			fmt.Fprintln(cmd.ErrOrStderr(), "warning: --all-tags is not supported")
		}
		if quiet {
			fmt.Fprintln(cmd.ErrOrStderr(), "warning: --quiet is not supported")
		}
		if disableContentTrust {
			fmt.Fprintln(cmd.ErrOrStderr(), "warning: --disable-content-trust is ignored")
		}
		if !unpack {
			fmt.Fprintln(cmd.ErrOrStderr(), "warning: --unpack=false is not supported")
		}

		db, err := newDb()
		if err != nil {
			return err
		}

		registry, img, tag, err := builder.ParseOciImage(image)
		if err != nil {
			return err
		}

		ociArch, err := builder.ToOciArchitecture(config.HostArchitecture)
		if err != nil {
			return err
		}

		def := builder.Factory.NewFetchOCIImageDefinition(registry, img, tag, ociArch)
		_, err = db.Builder().Build(def, common.BuildOptions{})
		return err
	},
}

func init() {
	ociCmd.AddCommand(ociRunCmd)
	ociCmd.AddCommand(ociPullCmd)
	rootCmd.AddCommand(ociCmd)
}
