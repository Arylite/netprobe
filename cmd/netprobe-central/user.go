package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/Arylite/netprobe/internal/central/auth"
	"github.com/Arylite/netprobe/internal/central/store"
)

// stdin is where passwords are read from; tests replace it.
var stdin io.Reader = os.Stdin

func userCommand(args []string, out io.Writer) error {
	if len(args) == 0 {
		return errUsage
	}
	switch args[0] {
	case "add":
		return userAdd(args[1:], out)
	case "list":
		return userList(args[1:], out)
	case "passwd":
		return userPasswd(args[1:], out)
	case "delete":
		return userDelete(args[1:], out)
	}
	return fmt.Errorf("%w: unknown user command %q", errUsage, args[0])
}

// readPassword takes the first line of the input. A password is never a flag:
// command lines are visible to every user of the machine.
func readPassword(r io.Reader) (string, error) {
	line, err := bufio.NewReader(io.LimitReader(r, auth.MaxPasswordLength+2)).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	password := strings.TrimRight(line, "\r\n")
	if password == "" {
		return "", errors.New("no password on standard input: pipe it in, as in: printf '%s\\n' 'a long password' | netprobe-central user add --username NAME --role admin")
	}
	return password, nil
}

func userFlags(cmd string, args []string, wantRole bool) (st *store.Store, username, role string, err error) {
	fs := flag.NewFlagSet("user "+cmd, flag.ContinueOnError)
	databaseURL := databaseFlag(fs)
	name := fs.String("username", "", "name of the account")
	roleFlag := fs.String("role", "", "admin or viewer")
	if err := fs.Parse(args); err != nil {
		return nil, "", "", err
	}
	if *name == "" {
		return nil, "", "", errors.New("--username is required")
	}
	if wantRole && !store.ValidRole(*roleFlag) {
		return nil, "", "", errors.New("--role must be admin or viewer")
	}
	st, err = openStore(context.Background(), *databaseURL)
	if err != nil {
		return nil, "", "", err
	}
	return st, *name, *roleFlag, nil
}

func userAdd(args []string, out io.Writer) error {
	st, username, role, err := userFlags("add", args, true)
	if err != nil {
		return err
	}
	defer st.Close()
	if err := store.ValidateUsername(username); err != nil {
		return err
	}
	password, err := readPassword(stdin)
	if err != nil {
		return err
	}
	if err := auth.ValidatePassword(username, password); err != nil {
		return err
	}
	hash, err := auth.HashPassword(password)
	if err != nil {
		return err
	}
	if err := st.AddUser(context.Background(), username, role, hash); err != nil {
		return err
	}
	fmt.Fprintf(out, "user %s created with the role %s\n", username, role)
	return nil
}

func userPasswd(args []string, out io.Writer) error {
	st, username, _, err := userFlags("passwd", args, false)
	if err != nil {
		return err
	}
	defer st.Close()
	password, err := readPassword(stdin)
	if err != nil {
		return err
	}
	if err := auth.ValidatePassword(username, password); err != nil {
		return err
	}
	hash, err := auth.HashPassword(password)
	if err != nil {
		return err
	}
	if err := st.SetPasswordHash(context.Background(), username, hash); err != nil {
		return err
	}
	fmt.Fprintf(out, "password of %s changed, its sessions are closed\n", username)
	return nil
}

func userDelete(args []string, out io.Writer) error {
	st, username, _, err := userFlags("delete", args, false)
	if err != nil {
		return err
	}
	defer st.Close()
	if err := st.DeleteUser(context.Background(), username); err != nil {
		return err
	}
	fmt.Fprintf(out, "user %s deleted\n", username)
	return nil
}

func userList(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("user list", flag.ContinueOnError)
	databaseURL := databaseFlag(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	st, err := openStore(context.Background(), *databaseURL)
	if err != nil {
		return err
	}
	defer st.Close()
	users, err := st.ListUsers(context.Background())
	if err != nil {
		return err
	}
	w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "USERNAME\tROLE\tCREATED")
	for _, u := range users {
		fmt.Fprintf(w, "%s\t%s\t%s\n", u.Username, u.Role, u.CreatedAt.Format("2006-01-02 15:04"))
	}
	return w.Flush()
}
