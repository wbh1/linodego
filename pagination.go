package linodego

/**
 * Pagination and Filtering types and helpers
 */

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"

	"github.com/go-resty/resty/v2"
)

// PageOptions are the pagination parameters for List endpoints
type PageOptions struct {
	Page    int `url:"page,omitempty" json:"page"`
	Pages   int `url:"pages,omitempty" json:"pages"`
	Results int `url:"results,omitempty" json:"results"`
}

// ListOptions are the pagination and filtering (TODO) parameters for endpoints
type ListOptions struct {
	*PageOptions
	PageSize    int    `json:"page_size"`
	Filter      string `json:"filter"`
	QueryParams any
}

// NewListOptions simplified construction of ListOptions using only
// the two writable properties, Page and Filter
func NewListOptions(page int, filter string) *ListOptions {
	return &ListOptions{PageOptions: &PageOptions{Page: page}, Filter: filter}
}

// Hash returns the sha256 hash of the provided ListOptions.
// This is necessary for caching purposes.
func (l ListOptions) Hash() (string, error) {
	data, err := json.Marshal(l)
	if err != nil {
		return "", fmt.Errorf("failed to cache ListOptions: %w", err)
	}

	h := sha256.New()

	h.Write(data)

	return fmt.Sprintf("%x", h.Sum(nil)), nil
}

func applyListOptionsToRequest(opts *ListOptions, req *resty.Request) error {
	if opts != nil {
		if opts.QueryParams != nil {
			params, err := flattenQueryStruct(opts.QueryParams)
			if err != nil {
				return fmt.Errorf("failed to apply list options: %w", err)
			}

			req.SetQueryParams(params)
		}

		if opts.PageOptions != nil && opts.Page > 0 {
			req.SetQueryParam("page", strconv.Itoa(opts.Page))
		}

		if opts.PageSize > 0 {
			req.SetQueryParam("page_size", strconv.Itoa(opts.PageSize))
		}

		if len(opts.Filter) > 0 {
			req.SetHeader("X-Filter", opts.Filter)
		}
	}

	return nil
}

type PagedResponse interface {
	endpoint(...any) string
	castResult(*resty.Request, string) (int, int, error)
}

// listHelper abstracts fetching and pagination for GET endpoints that
// do not require any Ids (top level endpoints).
// When opts (or opts.Page) is nil, all pages will be fetched and
// returned in a single (endpoint-specific)PagedResponse
// opts.results and opts.pages will be updated from the API response
func (c *Client) listHelper(ctx context.Context, pager PagedResponse, opts *ListOptions, ids ...any) error {
	req := c.R(ctx)
	if err := applyListOptionsToRequest(opts, req); err != nil {
		return err
	}

	pages, results, err := pager.castResult(req, pager.endpoint(ids...))
	if err != nil {
		return err
	}
	if opts == nil {
		opts = &ListOptions{PageOptions: &PageOptions{Page: 0}}
	}
	if opts.PageOptions == nil {
		opts.PageOptions = &PageOptions{Page: 0}
	}
	if opts.Page == 0 {
		for page := 2; page <= pages; page++ {
			opts.Page = page
			if err := c.listHelper(ctx, pager, opts, ids...); err != nil {
				return err
			}
		}
	}

	opts.Results = results
	opts.Pages = pages
	return nil
}

// flattenQueryStruct flattens a structure into a Resty-compatible query param map.
// Fields are mapped using the `query` struct tag.
func flattenQueryStruct(val any) (map[string]string, error) {
	result := make(map[string]string)

	reflectVal := reflect.ValueOf(val)

	// Deref pointer if necessary
	if reflectVal.Kind() == reflect.Ptr {
		reflectVal = reflect.Indirect(reflectVal)
	}

	valType := reflectVal.Type()

	for i := 0; i < valType.NumField(); i++ {
		currentField := valType.Field(i)

		queryTag, ok := currentField.Tag.Lookup("query")
		// Skip untagged fields
		if !ok {
			continue
		}

		valField := reflectVal.FieldByName(currentField.Name)
		if !valField.IsValid() {
			return nil, fmt.Errorf("invalid query param tag: %s", currentField.Name)
		}

		var resultField string

		switch valField.Interface().(type) {
		case string:
			resultField = valField.String()
		case int64, int:
			resultField = strconv.FormatInt(valField.Int(), 10)
		case bool:
			resultField = strconv.FormatBool(valField.Bool())
		default:
			return nil, fmt.Errorf("unsupported query param type: %s", valField.Type().Name())
		}

		result[queryTag] = resultField
	}

	return result, nil
}
