package zimbra

import (
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"

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

	// It seems to take a random amound of steps to log in
	// TODO: check for <form><div id="status" class="errors"> in output from
    //       https://auth.u-cergy.fr/login, indicating wrong login info
	for resp.Request.URL.Host != "mail.etu.cyu.fr" {
		slog.Debug("Extracting form informations")
		url, inputs, err := extractFormInfo(resp)
		if err != nil {
			return fmt.Errorf("cannot extract form informations: %w", err)
		}
		slog.Debug("Extracted form informations")

		slog.Info("Doing one login step", slog.String("url", url))
		resp, err = client.PostForm(url, inputs)
		if err != nil {
			return fmt.Errorf("POST %s: %w", url, err)
		}
		if resp.StatusCode != 200 {
			return fmt.Errorf("POST %s: unexpected status code: %v", url, resp.StatusCode)
		}
		if ct := resp.Header.Get("content-type"); !strings.HasPrefix(ct, "text/html") {
			return fmt.Errorf("POST %s: unexpected content-type: %s", url, ct)
		}
		slog.Debug("Did one login step", slog.Any("url", resp.Request.URL))
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

func extractFormInfo(resp *http.Response) (actionUrl string, inputs url.Values, err error) {
	doc, err := html.Parse(resp.Body)
	if err != nil {
		return
	}

	action, inputs, err := formInfo(doc)
	if err != nil {
		return
	}

	parsedAction, err := url.Parse(action)
	if err != nil {
		return
	}

	actionUrl = resp.Request.URL.ResolveReference(parsedAction).String()
	return
}

func formInfo(n *html.Node) (action string, inputs url.Values, err error) {
	if n.Type == html.ElementNode && n.Data == "form" {
		var method string

		for _, a := range n.Attr {
			switch a.Key {
			case "action":
				action = a.Val
			case "method":
				method = a.Val
			}
		}

		if strings.ToUpper(method) == "POST" {
			inputs = url.Values{}
			err = formInputs(n, inputs)
			return
		} else {
			slog.Debug("Form without method POST",
				slog.String("method", method),
				slog.String("action", action))
		}
	}

	for c := n.FirstChild; c != nil; c = c.NextSibling {
		action, inputs, err = formInfo(c)
		if err == nil {
			return
		}
	}

	err = fmt.Errorf("couldn't find form")
	return
}

func formInputs(n *html.Node, inputs url.Values) error {
	if n.Type == html.ElementNode && n.Data == "input" {
		typ := "text"
		var name string
		var value string

		for _, a := range n.Attr {
			switch a.Key {
			case "type":
				typ = a.Val
			case "name":
				name = a.Val
			case "value":
				value = a.Val
			}
		}

		switch typ {
		case "submit":
			fallthrough
		case "hidden":
			if name != "" && value != "" {
				inputs.Add(name, value)
				goto added
			}
		case "text":
			if name == "username" {
				inputs.Add("username", config.Username)
				goto added
			}
		case "password":
			if name == "password" {
				inputs.Add("password", config.Password)
				goto added
			}
		}

		slog.Debug("Ignored form input",
			slog.String("type", typ),
			slog.String("name", name),
			slog.String("value", value))

	added:
	}

	for c := n.FirstChild; c != nil; c = c.NextSibling {
		err := formInputs(c, inputs)
		if err != nil {
			return err
		}
	}

	return nil
}
