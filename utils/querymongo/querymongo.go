// Copyright 2024  Pantacor Ltd.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
//	Unless required by applicable law or agreed to in writing, software
//	distributed under the License is distributed on an "AS IS" BASIS,
//	WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//	See the License for the specific language governing permissions and
//	limitations under the License.

package querymongo

import (
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"gitlab.com/pantacor/pantahub-base/utils"
	"gitlab.com/pantacor/pantahub-base/utils/models"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const DefaultPageSize = 100

// Pagination paginable response
type Pagination struct {
	PageSizes   []int  `json:"-"`
	ResourceURL string `json:"resource"`
	PageSize    int    `json:"page_size"`
	PageOffset  int    `json:"page_offset"`
	CurrentPage int    `json:"current_page"`
	Total       int    `json:"total"`
	Next        string `json:"next"`
	Prev        string `json:"prev"`
}

type ApiSearchPagination struct {
	Filters    bson.M
	Sort       bson.M
	Fields     bson.M
	Pagination bson.M
	Url        url.URL
}

// MergeDefaultProjection merge projection with required values
func MergeDefaultProjection(p map[string]interface{}) map[string]interface{} {
	inclusionProjection := false
	for _, val := range p {
		if val == 1 {
			inclusionProjection = true
			break
		}
	}

	projection := map[string]interface{}{}
	if inclusionProjection {
		projection["_id"] = 1
		projection["time-created"] = 1
		projection["time-modified"] = 1
		projection["owner"] = 1
	}

	for key, val := range p {
		projection[key] = val
	}

	return projection
}

func GetAllQueryPagination(url *url.URL, searchable map[string]bool) ApiSearchPagination {
	asp := ApiSearchPagination{
		Url: *url,
	}

	asp.Filters = GetMongoQueryFromQuery(url.Query(), searchable)
	asp.Sort = GetMongoSortingFromQuery(url.Query())
	asp.Fields = GetMongoFieldsFromQuery(url.Query())
	asp.Pagination = GetMongoPaginationFromQuery(url.Query())

	if _, ok := asp.Pagination["offset"]; !ok {
		asp.Pagination["offset"] = 0
		query := url.Query()
		query.Add("page[offset]", "0")
		url.RawQuery = query.Encode()
	}

	return asp
}

// GetMongoSortingFromQuery get mongo sorting from query
func GetMongoSortingFromQuery(querystring url.Values) bson.M {
	sortBy := bson.M{}

	for queryKey, value := range querystring {
		if value == nil {
			continue
		}
		if !strings.Contains(queryKey, "sort_by") {
			continue
		}

		for _, key := range value {
			match := strings.SplitN(key, ":", 2)
			switch match[0] {
			case "asc":
				sortBy[match[1]] = 1
			case "desc":
				sortBy[match[1]] = -1
			default:
				sortBy[key] = 1
			}
		}
	}
	return sortBy
}

// GetMongoPaginationFromQuery get mongo pagination from query
func GetMongoPaginationFromQuery(querystring url.Values) bson.M {
	pagination := bson.M{}

	for queryKey, value := range querystring {
		if value == nil {
			continue
		}
		if !strings.Contains(queryKey, "page") {
			continue
		}

		if queryKey == "page[size]" {
			pagination["limit"] = processValue(value[0])
		}

		if queryKey == "page[after]" {
			pagination["after"] = processValue(value[0])
		}

		if queryKey == "page[offset]" {
			pagination["offset"] = processValue(value[0])
		}

		if queryKey == "page[before]" {
			pagination["before"] = processValue(value[0])
		}
	}
	return pagination
}

// GetMongoFieldsFromQuery get mongo fields from query
func GetMongoFieldsFromQuery(querystring url.Values) bson.M {
	selectionFields := bson.M{}
	re := regexp.MustCompile(`([+-])(.*)`)

	for key, value := range querystring {
		if value == nil {
			continue
		}
		if !strings.Contains(key, "fields") {
			continue
		}

		for _, v := range value {
			fields := strings.Split(v, ",")
			for _, field := range fields {
				match := re.FindStringSubmatch(field)
				if len(match) == 3 {
					switch match[1] {
					case "-":
						selectionFields[match[2]] = 0
					case "+":
						selectionFields[match[2]] = 1
					default:
						selectionFields[match[2]] = 1
					}
				} else {
					selectionFields[field] = 1
				}
			}
		}
	}
	return selectionFields
}

// GetMongoQueryFromQuery get mongo query from url query
func GetMongoQueryFromQuery(querystring url.Values, searchable map[string]bool) bson.M {
	query := bson.M{}

	for key, value := range querystring {
		_, ok := searchable[key]
		if value == nil || !ok {
			continue
		}

		isNotQuery :=
			strings.Contains(key, "fields") ||
				strings.Contains(key, "sort_by") ||
				strings.Contains(key, "page")

		if isNotQuery {
			continue
		}

		if len(value) > 1 {
			query[key] = bson.M{
				"$all": value,
			}
			continue
		}

		field := value[0]
		match := strings.SplitN(field, ":", 2)

		switch match[0] {
		case "in":
			values := strings.Split(match[1], ",")
			arr := make([]interface{}, len(values))
			for index, v := range values {
				arr[index] = processValue(v)
			}
			query[key] = bson.M{
				"$in": arr,
			}
		case "nin":
			values := strings.Split(match[1], ",")
			arr := make([]interface{}, len(values))
			for index, v := range values {
				arr[index] = processValue(v)
			}
			query[key] = bson.M{
				"$nin": arr,
			}
		case "exists":
			query[key] = bson.M{
				"$exists": processValue(match[1]),
			}
		case "eq":
			query[key] = bson.M{
				"$eq": processValue(match[1]),
			}
		case "ne":
			query[key] = bson.M{
				"$ne": processValue(match[1]),
			}
		case "lt":
			query[key] = bson.M{
				"$lt": processValue(match[1]),
			}
		case "lte":
			query[key] = bson.M{
				"$lte": processValue(match[1]),
			}
		case "gt":
			query[key] = bson.M{
				"$gt": processValue(match[1]),
			}
		case "gte":
			query[key] = bson.M{
				"$gte": processValue(match[1]),
			}
		case "all":
			query[key] = bson.M{
				"$all": strings.Split(match[1], ","),
			}
		case "empty":
			query[key] = bson.M{
				"$eq": "",
			}
		default:
			query[key] = processValue(field)
		}
	}

	return query
}

// SetMongoPagination set pagination
func SetMongoPagination(q, s bson.M, pa map[string]interface{}, queryOptions *options.FindOptions) {
	limit := int64(-1)
	if pa != nil {
		if l, ok := pa["limit"]; ok {
			limit = int64(l.(int))
		}
		if after, ok := pa["after"]; ok {
			s["created_at"] = -1
			if _, ok := q["created_at"]; ok {
				q["created_at"].(bson.M)["$lt"] = after
			} else {
				q["created_at"] = bson.M{
					"$lt": after,
				}
			}
		}
		if before, ok := pa["before"]; ok {
			s["created_at"] = -1
			if _, ok := q["created_at"]; ok {
				q["created_at"].(bson.M)["$gt"] = before
			} else {
				q["created_at"] = bson.M{
					"$gt": before,
				}
			}
		}
		if offset, ok := pa["offset"]; ok {
			queryOptions.SetSkip(int64(offset.(int)))
		} else {
			queryOptions.SetSkip(0)
		}
	}

	if limit > 0 {
		queryOptions.SetLimit(limit)
	}

	mongoSort := bson.D{}
	for key, value := range s {
		mongoSort = append(mongoSort, bson.E{Key: key, Value: value})
	}
	queryOptions.SetSort(mongoSort)
}

func processValue(v string) interface{} {
	var r interface{} = v

	if time, err := time.Parse(time.RFC3339, v); err == nil {
		return time
	}

	if i, err := strconv.Atoi(v); err == nil {
		return i
	}

	if b, err := strconv.ParseBool(v); err == nil {
		return b
	}

	if v == "null" {
		return nil
	}

	return r
}

func GetPaginationWithLink(u url.URL, total int64, last, first models.DatableSimple) Pagination {
	result := Pagination{
		Total:     int(total),
		PageSizes: []int{10, 20, 30, 50, 100},
	}

	finishTimestamp := last.GetCreatedAt().Format(time.RFC3339)
	startTimetamp := first.GetCreatedAt().Format(time.RFC3339)
	size := u.Query().Get("page[size]")
	if size == "" {
		size = strconv.Itoa(DefaultPageSize)
	}

	sizeInt, err := strconv.Atoi(size)
	if err != nil {
		sizeInt = DefaultPageSize
	}
	result.PageSize = sizeInt

	newURL, err := url.Parse(
		fmt.Sprintf(
			"%s://%s:%s",
			utils.GetEnv(utils.EnvPantahubScheme),
			utils.GetEnv(utils.EnvPantahubHost),
			utils.GetEnv(utils.EnvPantahubPort),
		),
	)
	if err != nil {
		newURL = &u
	}
	newURL.Path = u.Path

	result.ResourceURL = newURL.String()

	newURL.RawQuery = u.Query().Encode()

	if u.Query().Get("page[offset]") == "" {
		prevURL := *newURL
		prevQuery := prevURL.Query()
		prevQuery.Set("page[size]", size)
		prevQuery.Set("page[before]", startTimetamp)
		prevURL.RawQuery = prevQuery.Encode()
		result.Prev = prevURL.String()

		if int(result.Total) >= sizeInt {
			nextURL := *newURL
			nextQuery := nextURL.Query()
			nextQuery.Set("page[size]", size)
			nextQuery.Set("page[after]", finishTimestamp)
			nextURL.RawQuery = nextQuery.Encode()
			result.Next = nextURL.String()
		}
	} else {
		offset, err := strconv.Atoi(u.Query().Get("page[offset]"))
		if err != nil {
			offset = 0
		}

		current := offset / sizeInt

		result.PageOffset = offset
		result.CurrentPage = current + 1

		prevURL := *newURL
		prevQuery := prevURL.Query()

		prevOffsetInt := offset - sizeInt
		if prevOffsetInt >= 0 {
			prevQuery.Set("page[offset]", strconv.Itoa(prevOffsetInt))
		}
		prevQuery.Set("page[size]", size)
		prevURL.RawQuery = prevQuery.Encode()
		result.Prev = prevURL.String()

		if int(result.Total) >= sizeInt*current {
			nextOffset := strconv.Itoa(offset + sizeInt)

			nextURL := *newURL
			nextQuery := nextURL.Query()
			nextQuery.Set("page[size]", size)
			nextQuery.Set("page[offset]", nextOffset)
			nextURL.RawQuery = nextQuery.Encode()

			result.Next = nextURL.String()
		}
	}

	return result
}
