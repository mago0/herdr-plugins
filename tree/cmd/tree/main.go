// Command tree shows Herdr agents as a tree by supervisor.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sync/atomic"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mago0/herdr-plugins/tree/internal/model"
	"github.com/mago0/herdr-plugins/tree/internal/source"
	"github.com/mago0/herdr-plugins/tree/internal/view"
)

const onceWidth = 100

func main() {
	once := flag.Bool("once", false, "print the tree as plain text and exit")
	flag.Parse()

	path := os.Getenv("HERDR_SOCKET_PATH")
	if path == "" {
		fmt.Fprintln(os.Stderr, "tree: not in a Herdr pane (HERDR_SOCKET_PATH is not set)")
		os.Exit(1)
	}
	sock := source.Socket{Path: path}
	// The focused pane and tab of the last snapshot.
	var current atomic.Pointer[[2]string]
	load := func() (model.Tree, error) {
		snap, err := source.Snapshot(sock)
		if err != nil {
			return model.Tree{}, err
		}
		current.Store(&[2]string{snap.FocusedPane, snap.FocusedTab})
		return model.Build(snap, source.Dispatch(source.StateDir())), nil
	}
	theme := view.LoadTheme(os.Getenv("HERDR_PLUGIN_CONFIG_DIR"))
	stateDir := os.Getenv("HERDR_PLUGIN_STATE_DIR")
	folds := map[string]bool{}
	if stateDir != "" {
		folds = view.LoadFolds(stateDir)
	}

	if *once {
		t, err := load()
		if err != nil {
			fmt.Fprintln(os.Stderr, "tree:", err)
			os.Exit(1)
		}
		theme.Plain = true
		for _, line := range view.Render(view.Rows(t, view.State{Folded: folds}), onceWidth, -1, theme) {
			fmt.Println(line)
		}
		return
	}

	// The pane and tab the tree was opened from, so the cursor starts there.
	var origin struct {
		Pane string `json:"focused_pane_id"`
		Tab  string `json:"tab_id"`
	}
	json.Unmarshal([]byte(os.Getenv("HERDR_PLUGIN_CONTEXT_JSON")), &origin)

	deps := view.Deps{
		Load:       load,
		Focus:      func(n *model.Node) error { return source.Focus(sock, n) },
		OriginPane: origin.Pane,
		OriginTab:  origin.Tab,
		// Herdr sets this for a pane it runs as a section of its sidebar.
		Sidebar: os.Getenv("HERDR_SIDEBAR_SECTION") != "",
		Current: func() (string, string) {
			if c := current.Load(); c != nil {
				return c[0], c[1]
			}
			return "", ""
		},
		Focused: func() (string, string) { return source.Focused(sock) },
	}
	options := []tea.ProgramOption{tea.WithAltScreen()}
	if deps.Sidebar {
		options = append(options, tea.WithMouseAllMotion())
	}
	if stateDir != "" {
		deps.Save = func(f map[string]bool, t model.Tree) { view.SaveFolds(stateDir, f, t) }
	}
	if _, err := tea.NewProgram(view.New(deps, folds, theme), options...).Run(); err != nil {
		fmt.Fprintln(os.Stderr, "tree:", err)
		os.Exit(1)
	}
}
