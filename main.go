// The command "mark" stages files (usually in a file called
// ~/.mark-staging) and executes commands on them as a batch.

package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
)

var (
	// -create, create staging area if it doesn't exist
	flagCreateStaging = true

	// -preserve, don't remove files under directory if directory added
	flagPreserveSubdirs = false

	// -retain, don't clear the staging area after "exec"
	flagRetainMark = false

	// -v, print commands before executing
	flagPrintCommand = false

	// -dry, print but don't execute commands
	flagDryRun = false

	// -tag foo, apply commands only to files tagged "foo"
	flagTagMatch = ""

	// -staging ~/.other-staging, use different staging area
	flagStagingPath = "~/.mark-staging"

	availableCommands = `Available commands:
  add <files>
  exec (like, exec cp _ .)
  tag <tag> (files)
  remove (files)
  -help
`
)

func eprintf(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
}

func ok(err error) bool {
	if err != nil {
		eprintf("unexpected error: %s", err)
		return false
	}
	return true
}

func hardfail(err error) {
	if err != nil {
		eprintf("untenable error: %s", err)
		os.Exit(1)
	}
}

type Mark struct {
	Path string
	Tags []string

	Stage *StagingArea
}

type StagingArea struct {
	Marks []Mark
	path  string
}

func (s *StagingArea) Output(out []byte) {
	// fow now, but something smarter when parallel
	os.Stdout.Write(out)
}

// the crap we write at the top of every staging file
func prefix(out io.Writer) {
	cmd := strings.Trim(
		filepath.Base(os.Args[0])+
			" "+
			strings.Join(os.Args[1:], " "), " ")

	fmt.Fprintf(out, `
# this file was automatically created by "%s"
# you can edit it and mark will still work properly, but
# mark will happily overwrite it as well.

`, strings.Trim(cmd, " "))

}

// create an empty staging file
func createStaging(path string) (*StagingArea, error) {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return nil, err
	}

	prefix(f)

	f.Close()

	return &StagingArea{path: path}, nil
}

// GetStagingArea reads and parses the staging file, or create and returns
// a new one if none exists
func GetStagingArea(path string) (*StagingArea, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) && flagCreateStaging {
			return createStaging(path)
		} else {
			hardfail(err)
		}
	}

	defer f.Close()

	reader := bufio.NewReader(f)

	ret := &StagingArea{path: path}

	for {
		if line, eof := reader.ReadString('\n'); eof != nil {
			break
		} else if line[0] == '\n' || line[0] == ' ' || line[0] == '#' {
			continue
		} else {
			toks := strings.Fields(line)

			ret.Marks = append(ret.Marks, Mark{
				Stage: ret,
				Path:  toks[0],
				Tags:  toks[1:],
			})
		}
	}

	return ret, nil
}

// Remove removes all files from the staging area whose
// basename matches the glob pattern
// BUG(tqbf): this sucks, do something better than basename
func (s *StagingArea) Remove(glob string) int {
	newMarks := []Mark{}
	killed := 0

	for _, m := range s.Marks {
		hit, _ := filepath.Match(glob, path.Base(m.Path))
		if !hit {
			newMarks = append(newMarks, m)
		} else {
			killed++
		}
	}

	s.Marks = newMarks

	return killed
}

// Add adds a path to the staging area. If -preserve isn't
// set, adding a directory that is a parent to other files
// already in the staging area replaces those files with the
// directory itself.
// BUG(tqbf): make directory walks work
func (s *StagingArea) Add(path string) bool {
	path, err := filepath.Abs(path)
	if !ok(err) {
		return false
	}

	newDir := strings.HasSuffix(path, "/")
	kill := map[int]bool{}

	for i, m := range s.Marks {
		// same as staged path, do nothing
		if m.Path == path {
			return false
		}

		oldDir := strings.HasSuffix(m.Path, "/")

		// file already referenced by staged dir, do nothing
		if oldDir && strings.HasPrefix(path, m.Path) {
			return false
		}

		// path contained under new directory, no longer
		// need specific mark
		if newDir && strings.HasPrefix(m.Path, path) {
			if !flagPreserveSubdirs {
				kill[i] = true
			}
		}
	}

	newMark := []Mark{}

	for i, m := range s.Marks {
		if !kill[i] {
			newMark = append(newMark, m)
		}
	}

	newMark = append(newMark, Mark{
		Stage: s,
		Path:  path,
	})

	s.Marks = newMark

	return true
}

// Exec executes a command for a mark (unless -dry is set, in which
// case just print the command)
func (m *Mark) Exec(args []string) (err error) {
	nargs := []string{}

	for _, arg := range args {
		switch arg {
		case "_":
			nargs = append(nargs, m.Path)

		case "_.base":
			nargs = append(nargs, path.Base(m.Path))

		case "_.dir":
			nargs = append(nargs, path.Dir(m.Path))

		default:
			nargs = append(nargs, arg)
		}
	}

	args = nargs

	if flagDryRun || flagPrintCommand {
		fmt.Printf("sh -c %s\n", strings.Join(args, " "))
		if flagDryRun {
			return nil
		}
	}

	cmd := exec.Command("sh", "-c", strings.Join(args, " "))
	out, err := cmd.CombinedOutput()
	if err != nil {
		return err
	}

	m.Stage.Output(out)

	return nil
}

// Rewrite dumps the current parsed staging area back to disk
func (s *StagingArea) Rewrite() {
	f, err := ioutil.TempFile("", "mark")
	hardfail(err)

	prefix(f)

	for _, m := range s.Marks {
		io.WriteString(f, m.Path)

		for _, t := range m.Tags {
			io.WriteString(f, " "+t)
		}

		io.WriteString(f, "\n")
	}

	fn := f.Name()
	f.Close()

	hardfail(os.Rename(fn, s.path))
}

// Exec executes the command "args" across all files in the
// staging area; if tag is nonempty, only files matching tag
// are acted on
func (s *StagingArea) Exec(args []string, tag string) (completed int, rerr error) {
	for _, m := range s.Marks {
		if tag != "" {
			f := false
			for _, t := range m.Tags {
				if t == tag {
					f = true
					break
				}
			}

			if !f {
				continue
			}
		}

		err := m.Exec(args)
		if !ok(err) {
			rerr = err
		} else {
			completed += 1
		}
	}

	return completed, rerr
}

// Tag adds a tag to all files in the staging area whose
// basenmae matches pat. If "pat" is empty, all files are
// tagged, which might make sense if you're going to build
// up staging area incrementally.
// BUG(tqbf): again with this stupid basename stuff
func (m *Mark) Tag(pat, tag string) bool {
	hit, _ := filepath.Match(pat, path.Base(m.Path))
	if pat == "" || hit {
		for _, t := range m.Tags {
			if t == tag {
				return false
			}
		}

		m.Tags = append(m.Tags, tag)
		return true
	}

	return false
}

func status(stage *StagingArea) {
	eprintf(availableCommands)

	for i, m := range stage.Marks {
		fmt.Printf("%d. %s %v\n", i, m.Path, m.Tags)
	}

}

func main() {
	flag.BoolVar(&flagCreateStaging, "create", flagCreateStaging, "allow mark to create staging area")
	flag.BoolVar(&flagPreserveSubdirs, "preserve", flagPreserveSubdirs, "preserve subdirectories underneath newly added directory")
	flag.BoolVar(&flagRetainMark, "retain", flagRetainMark, "retain mark after execution")
	flag.BoolVar(&flagPrintCommand, "v", flagPrintCommand, "print commands before running")
	flag.BoolVar(&flagDryRun, "dry", flagDryRun, "print commands before running and don't run")
	flag.StringVar(&flagTagMatch, "tag", flagTagMatch, "match based on specified tag, not paths")
	flag.StringVar(&flagStagingPath, "staging", flagStagingPath, fmt.Sprintf("staging file (default: %s)", flagStagingPath))

	flag.Parse()

	flagStagingPath = strings.Replace(flagStagingPath, "~", os.Getenv("HOME"), -1)

	stage, err := GetStagingArea(flagStagingPath)
	hardfail(err)

	if len(flag.Args()) == 0 {
		status(stage)
		return
	}

	switch flag.Arg(0) {
	case "+":
		fallthrough
	case "add":
		added := 0

		paths := flag.Args()[1:]

		for _, path := range paths {
			if stage.Add(path) {
				added++
			}
		}

		if added > 0 {
			stage.Rewrite()
		}

	case "remove":
		removed := 0

		paths := flag.Args()[1:]

		if len(paths) == 0 {
			removed = len(stage.Marks)
			stage.Marks = []Mark{}
		} else {
			for _, path := range paths {
				removed += stage.Remove(path)
			}
		}

		if removed > 0 {
			stage.Rewrite()
		}

	case "tag":
		paths := flag.Args()[1:]
		if len(paths) == 0 {
			eprintf("mark tag <tag> (filenames)")
			return
		}

		tag := paths[0]
		paths = paths[1:]

		if len(paths) == 0 {
			for i, _ := range stage.Marks {
				stage.Marks[i].Tag("", tag)
			}
		} else {
			for _, path := range paths {
				for i, _ := range stage.Marks {
					stage.Marks[i].Tag(path, tag)
				}
			}
		}

		stage.Rewrite()

	case "exec":
		added := 0

		args := flag.Args()[1:]

		added, err := stage.Exec(args, flagTagMatch)
		fmt.Printf("%d of %d completed\n", added, len(stage.Marks))

		if !flagRetainMark && flagTagMatch == "" && !flagDryRun {
			stage.Marks = []Mark{}
			stage.Rewrite()
		}

		if err != nil {
			os.Exit(1)
		}

		return

	default:
		eprintf(availableCommands)
		return
	}
}
