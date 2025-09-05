package ghttp

import "net/url"

func ConstructURL(baseurl, path string) (string, error) {
	pathURL, err := url.Parse(path)
	if err != nil {
		return "", ErrInvalidPath
	}

	if pathURL.IsAbs() {
		return pathURL.String(), nil
	}

	if baseurl == "" {
		return "", ErrBaseUrlEmpty
	}

	baseURL, err := url.Parse(baseurl)
	if err != nil {
		return "", ErrBaseUrlFormat
	}

	mergedURL := baseURL.ResolveReference(pathURL)

	if !mergedURL.IsAbs() {
		return "", ErrUrlNotAbs
	}

	return mergedURL.String(), nil
}
