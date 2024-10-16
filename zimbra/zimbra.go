package zimbra

import (
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"

	"errors"

	"golang.org/x/net/html"
	"golang.org/x/net/publicsuffix"
	"ransan.fr/zimbridge/mda/config"
)

func Initialize() (*http.Client, error) {
	jar, err := cookiejar.New(&cookiejar.Options{PublicSuffixList: publicsuffix.List})
	if err != nil {
		return nil, fmt.Errorf("cookiejar.New: %w", err)
	}

	client := &http.Client{
		Jar: jar,
	}

	return client, nil
}

func Login(client *http.Client) error {
	slog.Info("Requesting login form")
	resp, err := client.Get("https://mail.etu.cyu.fr/")
	if err != nil {
		return fmt.Errorf("GET https://mail.etu.cyu.fr/: %w", err)
	}
	if resp.StatusCode != 200 {
		return fmt.Errorf("GET https://mail.etu.cyu.fr/: unexpected status code: %v", resp.StatusCode)
	}
	if ct := resp.Header.Get("content-type"); !strings.HasPrefix(ct, "text/html") {
		return fmt.Errorf("GET https://mail.etu.cyu.fr/: unexpected content-type: %s", ct)
	}
	slog.Debug("Got login form", slog.Any("url", resp.Request.URL))

	slog.Debug("Extracting form informations")
	form, err := getForm(resp.Body)
	if err != nil {
		return fmt.Errorf("cannot extract form informations: %w", err)
	}
	slog.Debug("Extracted form informations")

	slog.Info("Logging in", slog.String("username", config.Username))
	form.inputs.Add("username", config.Username)
	form.inputs.Add("password", config.Password)
	form.inputs.Add("submit", "SE CONNECTER")
	resp, err = client.PostForm("https://auth.u-cergy.fr"+form.url, form.inputs)
	if err != nil {
		return fmt.Errorf("POST https://auth.u-cergy.fr/: %w", err)
	}
	if resp.StatusCode != 200 {
		return fmt.Errorf("POST https://auth.u-cergy.fr/: unexpected status code: %v", resp.StatusCode)
	}
	if ct := resp.Header.Get("content-type"); !strings.HasPrefix(ct, "text/html") {
		return fmt.Errorf("POST https://auth.u-cergy.fr/: unexpected content-type: %s", ct)
	}
	slog.Debug("Logged in", slog.Any("url", resp.Request.URL))

	for resp.Request.URL.Host != "mail.etu.cyu.fr" {
		slog.Debug("Extracting form informations")
		form, err = getForm(resp.Body)
		if err != nil {
			return fmt.Errorf("cannot extract form informations: %w", err)
		}
		slog.Debug("Extracted form informations")

		slog.Info("Doing SSO stuff", slog.String("url", form.url))
		resp, err = client.PostForm(form.url, form.inputs)
		if err != nil {
			return fmt.Errorf("POST SSO: %w", err)
		}
		if resp.StatusCode != 200 {
			return fmt.Errorf("POST SSO: unexpected status code: %v", resp.StatusCode)
		}
		if ct := resp.Header.Get("content-type"); !strings.HasPrefix(ct, "text/html") {
			return fmt.Errorf("POST SSO: unexpected content-type: %s", ct)
		}
		slog.Debug("Did SSO stuff", slog.Any("url", resp.Request.URL))
	}

	return nil
}

func FetchArchive(client *http.Client) (io.ReadCloser, error) {
	slog.Info("Requesting tarball")
	resp, err := client.Get("https://mail.etu.cyu.fr/home/" + config.Address + "/?fmt=tgz")
	if err != nil {
		return nil, fmt.Errorf("GET https://mail.etu.cyu.fr/home/: %w", err)
	}
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("GET https://mail.etu.cyu.fr/home/: unexpected status code: %v", resp.StatusCode)
	}
	if ct := resp.Header.Get("content-type"); !strings.HasPrefix(ct, "application/x-compressed-tar") {
		return nil, fmt.Errorf("GET https://mail.etu.cyu.fr/home/: unexpected content-type: %s", ct)
	}
	slog.Debug("Got tarball", slog.Any("url", resp.Request.URL))

	return resp.Body, nil
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

func getForm(r io.Reader) (*form, error) {
	form := &form{
		inputs: url.Values{},
	}

	doc, err := html.Parse(r)
	if err != nil {
		return form, err
	}

	err = form.extractForm(doc)

	return form, err
}
