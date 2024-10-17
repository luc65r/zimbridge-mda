package main

import (
	"archive/tar"
	"compress/gzip"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path"
	"strings"

	"ransan.fr/zimbridge/mda/config"
	"ransan.fr/zimbridge/mda/maildir"
	"ransan.fr/zimbridge/mda/zimbra"
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

	flag.BoolVar(&config.Trash, "t", false, "")
	flag.BoolVar(&config.Trash, "trash", false, "")

	defaultVerbose := os.Getenv("ZIMBRIDGE_MDA_VERBOSE") == "1"
	var verboseFlag bool
	flag.BoolVar(&verboseFlag, "v", defaultVerbose, "")
	flag.BoolVar(&verboseFlag, "verbose", defaultVerbose, "")

	flag.Usage = func() {
		fmt.Printf(`zimbridge-mda %s
Lucas Ransan <lucas@ransan.fr>

Zimbridge-MDA (Zimbra bridge, Mail Delivery Agent) uses your USERNAME and your
PASSWORD to connect to https://mail.etu.cyu.fr (Zimbra webmail instance) and
download all your e-mails.  It stores them in the provided MAILDIR directory,
using Maildir++ directory layout.  You can then use an email client to read your
e-mails offline, or configure an IMAP server like Dovecot to use that directory.
Zimbridge-MDA can also move all the stored e-mails to the trash folder in the
webmail, so that it doesn't fetch them again the next time.

USAGE:
    %s -username USERNAME -password PASSWORD -address ADDRESS MAILDIR

POSITIONAL ARGUMENTS:
    <MAILDIR>

OPTIONS:
    -u, -username USERNAME    Your CYU username, probably starting with "e-"
    -p, -password PASSWORD    Your CYU password
    -a, -address ADDRESS      Your @etu.cyu.fr e-mail address
    -t, -trash                Trash e-mails in your webmail
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

	config.Maildir = flag.Arg(0)
	if config.Maildir == "" {
		slog.Error("No maildir directory provided")
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
		slog.String("maildir", config.Maildir))

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

	// Would it be better to request an uncompressed tar?
	// HTTP should compress it for transport
	zr, err := gzip.NewReader(archive)
	if err != nil {
		slog.Error("Couldn't read Gzip stream", slog.Any("error", err))
		os.Exit(1)
	}

	maildir, err := maildir.Open(config.Maildir)
	if err != nil {
		slog.Error("Failed to open maildir", slog.Any("error", err))
		os.Exit(1)
	}

	ids, err := storeMails(maildir, zr)
	if err != nil {
		slog.Error("Failed to store e-mails in maildir", slog.Any("error", err))
		os.Exit(1)
	}

	if config.Trash {
		err = zimbra.DeleteMails(client, ids)
		if err != nil {
			slog.Error("Failed to delete e-mails from Zimbra", slog.Any("error", err))
			os.Exit(1)
		}
	}
}

func storeMails(maildir *maildir.Maildir, zr io.Reader) ([]string, error) {
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
			parts := strings.Split(hdr.Name, "/")
			md := maildir
			for _, folder := range parts[:len(parts)-1] {
				md, err = md.AddFolder(folder)
				if err != nil {
					return nil, fmt.Errorf("open maildir folder: %w", err)
				}
			}

			slog.Debug("Writing e-mail", slog.String("name", hdr.Name))
			err = md.AddMail(tr)
			if err != nil {
				return nil, fmt.Errorf("write e-mail: %w", err)
			}

			name := parts[len(parts)-1]
			id, _, found := strings.Cut(name, "-")
			if !found {
				slog.Warn("Cannot find id in file name", slog.String("name", name))
				continue
			}

			id = strings.TrimLeft(id, "0")
			ids = append(ids, id)
		}
	}

	slog.Info(fmt.Sprintf("Stored %v e-mails", len(ids)))

	return ids, nil
}
