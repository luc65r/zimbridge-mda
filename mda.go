package main

import (
	"archive/tar"
	"compress/gzip"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"path"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/publicsuffix"
)

const (
	username = "e-lransan"
	password = "x1JHN5;S"
	address  = "lucas.ransan@etu.cyu.fr"
	maildir  = "/home/lucas/Mail"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))
	slog.SetDefault(logger)

	jar, err := cookiejar.New(&cookiejar.Options{PublicSuffixList: publicsuffix.List})
	if err != nil {
		slog.Error("Couldn't create cookie jar", slog.Any("error", err))
		os.Exit(1)
	}

	client := &http.Client{
		Jar: jar,
	}

	slog.Info("Requesting login form")
	resp, err := client.Get("https://mail.etu.cyu.fr/")
	if err != nil {
		slog.Error("Failed requesting login form", slog.Any("error", err))
		os.Exit(1)
	}
	if resp.StatusCode != 200 {
		slog.Error("Failed requesting login form", slog.Int("status", resp.StatusCode))
		os.Exit(1)
	}
	if ct := resp.Header.Get("content-type"); !strings.HasPrefix(ct, "text/html") {
		slog.Error("Failed requesting login from", slog.String("content-type", ct))
		os.Exit(1)
	}
	slog.Debug("Got login form", slog.Any("url", resp.Request.URL))

	slog.Debug("Extracting form informations")
	form, err := getForm(resp.Body)
	if err != nil {
		slog.Error("Couldn't extract form informations", slog.Any("error", err))
		os.Exit(1)
	}
	slog.Debug("Extracted form informations")

	slog.Info("Logging in", slog.String("username", username))
	form.inputs.Add("username", username)
	form.inputs.Add("password", password)
	form.inputs.Add("submit", "SE CONNECTER")
	resp, err = client.PostForm("https://auth.u-cergy.fr"+form.url, form.inputs)
	if err != nil {
		slog.Error("Couldn't log in", slog.Any("error", err))
		os.Exit(1)
	}
	if resp.StatusCode != 200 {
		slog.Error("Couldn't log in", slog.Int("status", resp.StatusCode))
		os.Exit(1)
	}
	if ct := resp.Header.Get("content-type"); !strings.HasPrefix(ct, "text/html") {
		slog.Error("Couldn't log in", slog.String("content-type", ct))
		os.Exit(1)
	}
	slog.Debug("Logged in", slog.Any("url", resp.Request.URL))

	for resp.Request.URL.Host != "mail.etu.cyu.fr" {
		slog.Debug("Extracting form informations")
		form, err = getForm(resp.Body)
		if err != nil {
			slog.Error("Couldn't extract form informations", slog.Any("error", err))
			os.Exit(1)
		}
		slog.Debug("Extracted form informations")

		slog.Info("SSO stuff")
		resp, err = client.PostForm(form.url, form.inputs)
		if err != nil {
			slog.Error("Couldn't SSO", slog.Any("error", err))
			os.Exit(1)
		}
		if resp.StatusCode != 200 {
			slog.Error("Couldn't SSO", slog.Int("status", resp.StatusCode))
			os.Exit(1)
		}
		if ct := resp.Header.Get("content-type"); !strings.HasPrefix(ct, "text/html") {
			slog.Error("Couldn't SSO", slog.String("content-type", ct))
			os.Exit(1)
		}
		slog.Debug("SSO stuff", slog.Any("url", resp.Request.URL))
	}

	slog.Info("Requesting tarball")
	resp, err = client.Get("https://mail.etu.cyu.fr/home/" + address + "/?fmt=tgz")
	if err != nil {
		slog.Error("Failed requesting tarball", slog.Any("error", err))
		os.Exit(1)
	}
	if resp.StatusCode != 200 {
		slog.Error("Failed requesting tarball", slog.Int("status", resp.StatusCode))
		os.Exit(1)
	}
	if ct := resp.Header.Get("content-type"); !strings.HasPrefix(ct, "application/x-compressed-tar") {
		slog.Error("Failed requesting tarball", slog.String("content-type", ct))
		os.Exit(1)
	}
	slog.Debug("Got tarball", slog.Any("url", resp.Request.URL))

	// Would it be better to request an uncompressed tar?
	// HTTP should compress it for transport
	zr, err := gzip.NewReader(resp.Body)
	if err != nil {
		slog.Error("Couldn't read Gzip stream", slog.Any("error", err))
		os.Exit(1)
	}

	slog.Info("Reading archive")
	tr := tar.NewReader(zr)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			slog.Error("Invalid tarball", slog.Any("error", err))
			os.Exit(1)
		}

		if hdr.Typeflag != tar.TypeReg {
			slog.Warn("Ignoring irregular file", slog.String("name", hdr.Name), slog.Int("type", int(hdr.Typeflag)))
			continue
		}

		if path.Ext(hdr.Name) == ".eml" {
			slog.Debug("In tarball", "name", hdr.Name)
		}
	}
}

type form struct {
	url    string
	inputs url.Values
}

func (form *form) extractForm(n *html.Node) error {
	if n.Type == html.ElementNode && n.Data == "form" {
		for _, a := range n.Attr {
			if a.Key == "action" {
				form.url = a.Val
				break
			}
		}

		return form.extractHiddenInputs(n)
	}

	for c := n.FirstChild; c != nil; c = c.NextSibling {
		err := form.extractForm(c)
		if err == nil {
			return nil
		}
	}

	return errors.New("couldn't find form")
}

func (form *form) extractHiddenInputs(n *html.Node) error {
	if n.Type == html.ElementNode && n.Data == "input" {
		hidden := false
		name := ""
		value := ""

		for _, a := range n.Attr {
			switch a.Key {
			case "type":
				hidden = a.Val == "hidden"
			case "name":
				name = a.Val
			case "value":
				value = a.Val
			}
		}

		if hidden && name != "" && value != "" {
			form.inputs.Add(name, value)
		}
	}

	for c := n.FirstChild; c != nil; c = c.NextSibling {
		form.extractHiddenInputs(c)
	}

	return nil
}

func getForm(r io.Reader) (form, error) {
	form := form{
		inputs: url.Values{},
	}

	doc, err := html.Parse(r)
	if err != nil {
		return form, err
	}

	err = form.extractForm(doc)

	return form, err
}
