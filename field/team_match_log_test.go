package field

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/team841/bioarena/model"
	"github.com/team841/bioarena/network"
	"github.com/stretchr/testify/assert"
)

// Logs must land in logs/, not static/logs/. Upstream writes them under static so they
// are downloadable from the web UI, but that directory is served without authentication
// and is copied wholesale by the deploy step, which pushed the dev machine's accumulated
// test-match logs to the Pi on every deploy.
func TestTeamMatchLogWritesOutsideStatic(t *testing.T) {
	baseDir := t.TempDir()
	original := model.BaseDir
	model.BaseDir = baseDir
	defer func() { model.BaseDir = original }()

	match := &model.Match{Type: model.Test, ShortName: "T", LongName: "Test Match"}
	var wifiStatus network.TeamWifiStatus

	matchLog, err := NewTeamMatchLog(254, match, &wifiStatus)
	assert.Nil(t, err)
	defer matchLog.logFile.Close()

	entries, err := os.ReadDir(filepath.Join(baseDir, "logs"))
	assert.Nil(t, err)
	assert.Len(t, entries, 1)
	assert.Contains(t, entries[0].Name(), "254")

	// Nothing should have been written under static/.
	_, err = os.Stat(filepath.Join(baseDir, "static", "logs"))
	assert.True(t, os.IsNotExist(err), "logs were written under static/")
}
