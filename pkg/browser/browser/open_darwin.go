package browser

import (
	"os"
	"os/exec"

	"github.com/pkg/browser"

	"github.com/tinyrange/tinyrange/pkg/common"
)

func Open(url string) error {
	if ok, _ := common.Exists("/Applications/Google Chrome.app/"); ok {
		cmd := exec.Command("open", "-n", "-a", "Google Chrome", "--args", "--app="+url)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		return cmd.Run()
	} else {
		return browser.OpenURL(url)
	}
}
