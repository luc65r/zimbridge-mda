package main

import (
	"archive/tar"
	"compress/gzip"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"path"
	"strings"

	"github.com/emersion/go-smtp"
	"ransan.fr/zimbridge/config"
	"ransan.fr/zimbridge/zimbra"
)

func main() {
	defaultUsername := os.Getenv("ZIMBRIDGE_MDA_USERNAME")
	flag.StringVar(&config.Username, "u", defaultUsername, "")
	flag.StringVar(&config.Username, "username", defaultUsername, "")

	defaultPassword := os.Getenv("ZIMBRIDGE_MDA_PASSWORD")
	flag.StringVar(&config.Password, "p", defaultPassword, "")
	flag.StringVar(&config.Password, "password", defaultPassword, "")

	defaultAddress := os.Getenv("ZIMBRIDGE_MDA_ADDRESS")
	flag.StringVar(&config.Address, "a", defaultAddress, "")
	flag.StringVar(&config.Address, "address", defaultAddress, "")

	defaultTag := os.Getenv("ZIMBRIDGE_MDA_TAG")
	flag.StringVar(&config.Tag, "t", defaultTag, "")
	flag.StringVar(&config.Tag, "tag", defaultTag, "")

	defaultVerbose := os.Getenv("ZIMBRIDGE_MDA_VERBOSE") == "1"
	var verboseFlag bool
	flag.BoolVar(&verboseFlag, "v", defaultVerbose, "")
	flag.BoolVar(&verboseFlag, "verbose", defaultVerbose, "")

	flag.Usage = func() {
		fmt.Printf(`zimbridge-mda %s
Lucas Ransan <lucas@ransan.fr>

Zimbridge-MDA (Zimbra bridge, Mail Delivery Agent) uses your USERNAME and your
PASSWORD to connect to https://mail.etu.cyu.fr (Zimbra webmail instance) and
download all e-mails from the Inbox folder.  It sends them to a provided
LMTP_SERVER, like Dovecot, using UNIX sockets.  Zimbridge-MDA can also tag all
the stored e-mails in the webmail, so that it doesn't fetch them again the next
time.

USAGE:
    %s -username USERNAME -password PASSWORD -address ADDRESS LMTP_SERVER

POSITIONAL ARGUMENTS:
    <LMTP_SERVER>    Path to UNIX socket where your LMTP server is listening

OPTIONS:
    -u, -username USERNAME    Your CYU username, probably starting with "e-"
    -p, -password PASSWORD    Your CYU password
    -a, -address ADDRESS      Your @etu.cyu.fr e-mail address
    -t, -tag TAG              Tag e-mails in your webmail
    -v, -verbose              Print debug informations
    -h, -help                 Print usage informations and quit
`, config.Version, os.Args[0])
	}

	flag.Parse()

	handlerOptions := slog.HandlerOptions{
		Level: slog.LevelInfo,
	}
	if verboseFlag {
		handlerOptions.Level = slog.LevelDebug
	}
	logger := slog.New(slog.NewTextHandler(os.Stdout, &handlerOptions))
	slog.SetDefault(logger)

	config.LMTPServer = flag.Arg(0)
	if config.LMTPServer == "" {
		slog.Error("No LMTP server provided")
		flag.Usage()
		os.Exit(1)
	}

	if flag.NArg() > 1 {
		slog.Error("Too many arguments")
		flag.Usage()
		os.Exit(1)
	}

	if config.Username == "" {
		slog.Error("No username provided")
		flag.Usage()
		os.Exit(1)
	}

	if config.Password == "" {
		slog.Error("No password provided")
		flag.Usage()
		os.Exit(1)
	}

	// TODO: fetch address from Zimbra
	if config.Address == "" {
		slog.Error("No address provided")
		flag.Usage()
		os.Exit(1)
	}

	slog.Debug("Starting",
		slog.String("username", config.Username),
		slog.String("password", strings.Repeat("*", len(config.Password))),
		slog.String("address", config.Address),
		slog.String("LMTP server", config.LMTPServer))

	client, err := zimbra.Initialize()
	if err != nil {
		slog.Error("Couldn't initialize Zimbra fetcher", slog.Any("error", err))
		os.Exit(1)
	}

	err = zimbra.Login(client)
	if err != nil {
		slog.Error("Couldn't login into Zimbra", slog.Any("error", err))
		os.Exit(1)
	}

	archive, err := zimbra.FetchArchive(client)
	if err != nil {
		slog.Error("Couldn't fetch archive", slog.Any("error", err))
		os.Exit(1)
	}
	if archive == nil {
		slog.Info("Nothing new")
		os.Exit(0)
	}

	// Would it be better to request an uncompressed tar?
	// HTTP should compress it for transport
	zr, err := gzip.NewReader(archive)
	if err != nil {
		slog.Error("Couldn't read Gzip stream", slog.Any("error", err))
		os.Exit(1)
	}

	lmtp, err := net.Dial("unix", config.LMTPServer)
	if err != nil {
		slog.Error("Failed to dial LTMP server", slog.Any("error", err))
		os.Exit(1)
	}
	defer lmtp.Close()

	lmtpClient := smtp.NewClientLMTP(lmtp)
	defer lmtpClient.Quit()

	ids, err := deliverMails(lmtpClient, zr)
	if err != nil {
		slog.Error("Failed to deliver e-mails to LMTP server", slog.Any("error", err))
		os.Exit(1)
	}

	if config.Tag != "" {
		err = zimbra.TagMails(client, ids)
		if err != nil {
			slog.Error("Failed to tag e-mails in Zimbra",
				slog.Any("error", err),
				slog.String("tag", config.Tag))
			os.Exit(1)
		}
	}
}

func deliverMails(client *smtp.Client, zr io.Reader) ([]string, error) {
	var ids []string

	slog.Info("Reading archive")
	tr := tar.NewReader(zr)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("invalid tarball: %w", err)
		}

		if hdr.Typeflag != tar.TypeReg {
			slog.Warn("Ignoring irregular file",
				slog.String("name", hdr.Name),
				slog.Int("type", int(hdr.Typeflag)))
			continue
		}

		if path.Ext(hdr.Name) == ".eml" {
			slog.Debug("Delivering e-mail", slog.String("name", hdr.Name))

			err = client.Mail("", nil)
			if err != nil {
				return nil, fmt.Errorf("LMTP MAIL: %w", err)
			}

			err = client.Rcpt(config.Address, nil)
			if err != nil {
				return nil, fmt.Errorf("LMTP RCPT: %w", err)
			}

			data, err := client.LMTPData(func(rcpt string, status *smtp.SMTPError) {
				if status != nil {
					slog.Warn("LMTP error", slog.String("rcpt", rcpt), slog.Any("status", *status))
				}
			})
			if err != nil {
				return nil, fmt.Errorf("LMTP DATA: %w", err)
			}

			_, err = io.Copy(data, tr)
			closeErr := data.Close()
			if err != nil {
				return nil, err
			}
			if closeErr != nil {
				return nil, fmt.Errorf("close data: %w", err)
			}

			err = client.Reset()
			if err != nil {
				return nil, err
			}

			parts := strings.Split(hdr.Name, "/")
			name := parts[len(parts)-1]
			id, _, found := strings.Cut(name, "-")
			if !found {
				slog.Error("Cannot find id in file name", slog.String("name", name))
				continue
			}

			id = strings.TrimLeft(id, "0")
			ids = append(ids, id)
		}
	}

	slog.Info(fmt.Sprintf("Stored %v e-mails", len(ids)))

	return ids, nil
}
