package toolresult

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"crdx.org/duckopt/v2"
	"golang.org/x/term"

	"crdx.org/io/cmd/oh/location"
	"crdx.org/io/internal/toolresult"
)

const usage = `ohctl tool-result — show a tool call's output

Usage:
    $0 tool-result [--pager] <url>

Options:
    -p, --pager    Show output in a pager
    -h, --help     Show this help
`

type inputOpts struct {
	ToolResult bool   `docopt:"tool-result"`
	Pager      bool   `docopt:"--pager"`
	URL        string `docopt:"<url>"`
}

type pager func(string) error

func Run() error {
	return run(duckopt.MustBind[inputOpts](usage, "$0"), location.GetSessionsDir(), os.Stdout, page)
}

func run(options *inputOpts, directory string, output io.Writer, openPager pager) error {
	exchange, err := toolresult.Read(directory, options.URL)
	if err != nil {
		return err
	}

	result := render(exchange, getColumns(output))
	if options.Pager {
		return openPager(result)
	}

	_, err = fmt.Fprint(output, result)
	return err
}

func getColumns(output io.Writer) int {
	file, isFile := output.(*os.File)
	if !isFile {
		return 0
	}
	columns, _, err := term.GetSize(int(file.Fd()))
	if err != nil {
		return 0
	}
	return columns
}

func page(text string) error {
	command := exec.CommandContext(context.Background(), "less", "-R", "-+F")
	command.Stdin = strings.NewReader(text)
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	if err := command.Run(); err != nil {
		return fmt.Errorf("pager failed: %w", err)
	}
	return nil
}
