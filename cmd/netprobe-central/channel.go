package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/Arylite/netprobe/internal/central/alert"
	"github.com/Arylite/netprobe/internal/central/store"
	"github.com/Arylite/netprobe/internal/cli"
	"github.com/Arylite/netprobe/internal/probe"
)

// sender delivers the test of 'channel test'; tests replace it.
var sender alert.Sender

func channelCommand(args []string, out io.Writer) error {
	if len(args) == 0 {
		return errUsage
	}
	switch args[0] {
	case "add":
		return channelAdd(args[1:], out)
	case "list":
		return channelList(args[1:], out)
	case "remove":
		return channelRemove(args[1:], out)
	case "test":
		return channelTest(args[1:], out)
	}
	return fmt.Errorf("%w: unknown channel command %q", errUsage, args[0])
}

// readSecret takes the first line of the input. Like a password, a secret is
// never a flag: command lines are visible to every user of the machine.
func readSecret(r io.Reader) (string, error) {
	line, err := bufio.NewReader(io.LimitReader(r, 1024)).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	secret := strings.TrimRight(line, "\r\n")
	if secret == "" {
		return "", errors.New("no secret on standard input: pipe it in, as in: printf '%s\\n' 'the secret' | netprobe-central channel add --name NAME --url URL --secret-stdin")
	}
	return secret, nil
}

func channelAdd(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("channel add", flag.ContinueOnError)
	databaseURL := databaseFlag(fs)
	name := fs.String("name", "", "name of the channel")
	url := fs.String("url", "", "the webhook, an http:// or https:// address that receives a JSON POST")
	secretStdin := fs.Bool("secret-stdin", false, "read from standard input the secret that signs what is sent")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if err := store.ValidateChannelName(*name); err != nil {
		return err
	}
	if err := store.ValidateChannelURL(*url); err != nil {
		return err
	}
	var secret string
	if *secretStdin {
		var err error
		if secret, err = readSecret(stdin); err != nil {
			return err
		}
	}
	st, err := openStore(context.Background(), *databaseURL)
	if err != nil {
		return err
	}
	defer st.Close()
	if _, err := st.AddChannel(context.Background(), *name, *url, secret); err != nil {
		return err
	}
	fmt.Fprintf(out, "channel %s added: it hears of the incidents that start from now on\n", *name)
	return nil
}

func channelList(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("channel list", flag.ContinueOnError)
	databaseURL := databaseFlag(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	st, err := openStore(context.Background(), *databaseURL)
	if err != nil {
		return err
	}
	defer st.Close()
	channels, err := st.ListChannels(context.Background())
	if err != nil {
		return err
	}
	// The address may hold a secret: print where it goes, not all of it.
	w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tHOST\tSIGNED\tCREATED")
	for _, c := range channels {
		signed := "no"
		if c.Secret != "" {
			signed = "yes"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", c.Name, hostOf(c.URL), signed, c.CreatedAt.Format("2006-01-02 15:04"))
	}
	return w.Flush()
}

func hostOf(raw string) string {
	rest := raw[strings.Index(raw, "://")+3:]
	host, _, _ := strings.Cut(rest, "/")
	return host
}

func channelRemove(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("channel remove", flag.ContinueOnError)
	databaseURL := databaseFlag(fs)
	name := fs.String("name", "", "name of the channel")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *name == "" {
		return errors.New("--name is required")
	}
	st, err := openStore(context.Background(), *databaseURL)
	if err != nil {
		return err
	}
	defer st.Close()
	if err := st.RemoveChannel(context.Background(), *name); err != nil {
		return err
	}
	fmt.Fprintf(out, "channel %s removed\n", *name)
	return nil
}

func channelTest(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("channel test", flag.ContinueOnError)
	databaseURL := databaseFlag(fs)
	name := fs.String("name", "", "name of the channel")
	deny := fs.String("webhook-deny", cli.Getenv("NETPROBE_WEBHOOK_DENY", strings.Join(probe.DefaultDeny, ",")), "ranges the webhook may not connect to, CIDRs separated by commas, or none (NETPROBE_WEBHOOK_DENY)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *name == "" {
		return errors.New("--name is required")
	}
	policy, err := probe.ParseDeny(*deny)
	if err != nil {
		return err
	}
	send := sender
	if send == nil {
		send = alert.NewWebhook(policy)
	}
	st, err := openStore(context.Background(), *databaseURL)
	if err != nil {
		return err
	}
	defer st.Close()
	ch, err := st.GetChannel(context.Background(), *name)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := alert.SendTest(ctx, send, ch); err != nil {
		return fmt.Errorf("channel %s did not accept the test: %w", *name, err)
	}
	fmt.Fprintf(out, "channel %s accepted the test\n", *name)
	return nil
}
