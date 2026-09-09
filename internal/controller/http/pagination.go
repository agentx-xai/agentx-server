package http

import (
	"agentx/server/internal/repo"
	"encoding/base64"
	"fmt"
	"strconv"

	"github.com/gin-gonic/gin"
)

type pageResult[T any] struct {
	Items      []T    `json:"items"`
	Count      int    `json:"count"`
	NextCursor string `json:"next_cursor,omitempty"`
}

func pageRequest(c *gin.Context) (repo.PageRequest, error) {
	limit := 50
	if raw, ok := c.GetQuery("limit"); ok {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 1 || value > 200 {
			return repo.PageRequest{}, fmt.Errorf("limit must be between 1 and 200")
		}
		limit = value
	}
	offset := 0
	if cursor, ok := c.GetQuery("cursor"); ok && cursor != "" {
		raw, err := base64.RawURLEncoding.DecodeString(cursor)
		if err != nil {
			return repo.PageRequest{}, fmt.Errorf("invalid cursor")
		}
		offset, err = strconv.Atoi(string(raw))
		if err != nil || offset < 0 {
			return repo.PageRequest{}, fmt.Errorf("invalid cursor")
		}
	}
	return repo.PageRequest{Limit: limit, Offset: offset}, nil
}

func writeRepositoryPage[T any](c *gin.Context, request repo.PageRequest, page repo.Page[T]) {
	next := ""
	if request.Offset+len(page.Items) < page.Total {
		next = base64.RawURLEncoding.EncodeToString([]byte(strconv.Itoa(request.Offset + len(page.Items))))
		c.Header("X-Next-Cursor", next)
	}
	if page.Items == nil {
		page.Items = []T{}
	}
	c.JSON(200, pageResult[T]{Items: page.Items, Count: page.Total, NextCursor: next})
}

func collectionPage(c *gin.Context, total int) (limit, offset int, paged bool, err error) {
	_, hasLimit := c.GetQuery("limit")
	cursor, hasCursor := c.GetQuery("cursor")
	paged = hasLimit || hasCursor
	limit = 50
	if hasLimit {
		limit, err = strconv.Atoi(c.Query("limit"))
		if err != nil || limit < 1 || limit > 200 {
			return 0, 0, paged, fmt.Errorf("limit must be between 1 and 200")
		}
	}
	if hasCursor && cursor != "" {
		raw, decodeErr := base64.RawURLEncoding.DecodeString(cursor)
		if decodeErr != nil {
			return 0, 0, paged, fmt.Errorf("invalid cursor")
		}
		offset, decodeErr = strconv.Atoi(string(raw))
		if decodeErr != nil || offset < 0 || offset > total {
			return 0, 0, paged, fmt.Errorf("invalid cursor")
		}
	}
	return limit, offset, paged, nil
}

func slicePage[T any](items []T, limit, offset int) ([]T, string) {
	if offset >= len(items) {
		return []T{}, ""
	}
	end := offset + limit
	if end > len(items) {
		end = len(items)
	}
	next := ""
	if end < len(items) {
		next = base64.RawURLEncoding.EncodeToString([]byte(strconv.Itoa(end)))
	}
	return items[offset:end], next
}

func writeCollection[T any](c *gin.Context, items []T) {
	limit, offset, _, err := collectionPage(c, len(items))
	if err != nil {
		c.JSON(400, errorEnvelope(c, "INVALID_PAGINATION", err.Error()))
		return
	}
	page, next := slicePage(items, limit, offset)
	if next != "" {
		c.Header("X-Next-Cursor", next)
	}
	c.JSON(200, pageResult[T]{Items: page, Count: len(items), NextCursor: next})
}
