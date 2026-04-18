//go:build darwin

package setter

import "context"

func newPlatform(opts Options) (Setter, error) {
	r := opts.Runner
	return &cmdSetter{
		name: "darwin/osascript",
		apply: func(ctx context.Context, path string) error {
			script := `tell application "System Events" to set picture of every desktop to POSIX file "` + path + `"`
			return r.Run(ctx, "osascript", "-e", script)
		},
	}, nil
}
