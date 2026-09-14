// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package github

import (
	"fmt"

	gogithub "github.com/google/go-github/v76/github"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
)

// listPages collects every page of a go-github list call, 100 items at a
// time, following the page number the response parses from the Link header.
// A next page that does not move past the current one is refused: a server
// repeating its Link header would otherwise be followed until the job timed
// out.
func listPages[T any](fetch func(opts *gogithub.ListOptions) ([]T, *gogithub.Response, error)) ([]T, error) {
	opts := &gogithub.ListOptions{PerPage: 100}

	var all []T

	for {
		items, resp, err := fetch(opts)
		if err != nil {
			return nil, err
		}

		all = append(all, items...)

		if resp == nil || resp.NextPage == 0 {
			return all, nil
		}

		if resp.NextPage <= opts.Page {
			return nil, fmt.Errorf("github list: next page %d does not advance past page %d: %w", resp.NextPage, opts.Page, errs.ErrMalformedInput)
		}

		opts.Page = resp.NextPage
	}
}
