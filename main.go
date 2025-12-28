package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
	"unicode"
)

const (
	authURL     = "https://lingualeo.com/api/auth"
	getWordsURL = "https://api.lingualeo.com/GetWords"

	email    = ""
	password = ""
)

type BodyAuth struct {
	Type        string              `json:"type"`
	Credentials BodyAuthCredentials `json:"credentials"`
}
type BodyAuthCredentials struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type GetWordResponse struct {
	APIVersion string `json:"apiVersion"`
	Data       []struct {
		GroupCount int    `json:"groupCount"`
		GroupName  string `json:"groupName"`
		Words      []struct {
			Association         interface{} `json:"association"`
			CombinedTranslation string      `json:"combinedTranslation"`
			Created             int         `json:"created"`
			ID                  int         `json:"id"`
			LearningStatus      int         `json:"learningStatus"`
			Origin              interface{} `json:"origin"`
			Picture             string      `json:"picture"`
			Progress            int         `json:"progress"`
			Pronunciation       string      `json:"pronunciation"`
			RelatedWords        interface{} `json:"relatedWords"`
			SpeechPartID        interface{} `json:"speechPartId"`
			Transcription       string      `json:"transcription"`
			Translations        interface{} `json:"translations"`
			WordLemmaID         interface{} `json:"wordLemmaId"`
			WordLemmaValue      interface{} `json:"wordLemmaValue"`
			WordSets            []struct {
				CountWords int    `json:"countWords"`
				ID         int    `json:"id"`
				Name       string `json:"name"`
			} `json:"wordSets"`
			WordType  int    `json:"wordType"`
			WordValue string `json:"wordValue"`
		} `json:"words"`
	} `json:"data"`
	ListWordSets []struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
	} `json:"listWordSets"`
	Status    string `json:"status"`
	Trainings []struct {
		IsPremium bool   `json:"isPremium"`
		Key       string `json:"key"`
		Title     string `json:"title"`
	} `json:"trainings"`
	WordList interface{} `json:"wordList"`
	WordSet  struct {
		CountWords int    `json:"countWords"`
		ID         int    `json:"id"`
		IsGlobal   bool   `json:"isGlobal"`
		Name       string `json:"name"`
	} `json:"wordSet"`
}
type GetWordRequest struct {
	APIVersion string `json:"apiVersion"`
	AttrList   struct {
		Association         string `json:"association"`
		CombinedTranslation string `json:"combinedTranslation"`
		Created             string `json:"created"`
		ID                  string `json:"id"`
		LearningStatus      string `json:"learningStatus"`
		ListWordSets        string `json:"listWordSets"`
		Origin              string `json:"origin"`
		Picture             string `json:"picture"`
		Progress            string `json:"progress"`
		Pronunciation       string `json:"pronunciation"`
		RelatedWords        string `json:"relatedWords"`
		SpeechPartID        string `json:"speechPartId"`
		Trainings           string `json:"trainings"`
		Transcription       string `json:"transcription"`
		Translations        string `json:"translations"`
		WordLemmaID         string `json:"wordLemmaId"`
		WordLemmaValue      string `json:"wordLemmaValue"`
		WordSets            string `json:"wordSets"`
		WordType            string `json:"wordType"`
		WordValue           string `json:"wordValue"`
	} `json:"attrList"`
	Category  string `json:"category"`
	DateGroup string `json:"dateGroup"`
	IDs       []struct {
		Y string `json:"y"`
	} `json:"iDs"`
	Mode      string      `json:"mode"`
	Offset    interface{} `json:"offset"`
	PerPage   int         `json:"perPage"`
	Search    string      `json:"search"`
	Status    string      `json:"status"`
	Training  interface{} `json:"training"`
	WordSetID int         `json:"wordSetId"`
}

func main() {
	// HTTP client with timeout
	client := &http.Client{Timeout: 20 * time.Second}

	rememberCookie, err := authenticate(context.Background(), client)
	if err != nil {
		fmt.Println("auth error:", err)
		os.Exit(1)
	}

	fmt.Printf("remember cookie: %s\n", rememberCookie)
	fmt.Println("Authorization successfully completed")

	// Prepare TSV output file
	f, err := os.Create("words.tsv")
	if err != nil {
		fmt.Println("cannot create TSV file:", err)
		os.Exit(1)
	}
	defer f.Close()

	ctx := context.Background()

	// Global deduplication structures
	seen := make(map[string]struct{})
	var dupCount int
	var skippedEmpty int

	// Initial request to obtain list of word sets (using set id 1 by default)
	initResp, err := getWordsPage(ctx, client, rememberCookie, 1, "start", nil, 30)
	if err != nil {
		fmt.Println("get words (initial) error:", err)
		os.Exit(1)
	}

	// Build list of all word set IDs to process
	type setInfo struct {
		id   int
		name string
	}
	var sets []setInfo
	// Ensure the currently used set (1) is included if not present in listWordSets
	setSeen := map[int]bool{}
	for _, s := range initResp.ListWordSets {
		sets = append(sets, setInfo{id: s.ID, name: s.Name})
		setSeen[s.ID] = true
	}
	if !setSeen[1] {
		sets = append(sets, setInfo{id: 1, name: ""})
	}

	// Process each set: print groups and counts, then fetch all words across all groups with pagination
	for _, si := range sets {
		// Fetch once with dateGroup=start to get group list and counts for this set
		setResp, err := getWordsPage(ctx, client, rememberCookie, si.id, "start", nil, 30)
		if err != nil {
			fmt.Println("  error fetching set header:", err)
			continue
		}

		// Determine WordSet name (prefer listWordSets entry, fallback to response.WordSet.Name)
		setName := si.name
		if setName == "" {
			if wsName := setResp.WordSet.Name; wsName != "" {
				setName = wsName
			}
		}
		fmt.Printf("WordSet %d %s\n", si.id, setName)

		// Print group names with counts (rename any group name to "main" for WordSet id=1)
		for _, grp := range setResp.Data {
			displayName := grp.GroupName
			//if si.id == 1 {
			//	displayName = "main"
			//}
			fmt.Printf("  group: %s, count: %d\n", displayName, grp.GroupCount)
		}

		// For each group name, paginate until empty and write TSV rows
		for _, grp := range setResp.Data {
			groupName := grp.GroupName
			var offset interface{} = nil
			for {
				pageResp, err := getWordsPage(ctx, client, rememberCookie, si.id, groupName, offset, 30)
				if err != nil {
					fmt.Println("  error page fetch:", err)
					break
				}
				// Find the matching group in the response
				var words []struct {
					Association         interface{} `json:"association"`
					CombinedTranslation string      `json:"combinedTranslation"`
					Created             int         `json:"created"`
					ID                  int         `json:"id"`
					LearningStatus      int         `json:"learningStatus"`
					Origin              interface{} `json:"origin"`
					Picture             string      `json:"picture"`
					Progress            int         `json:"progress"`
					Pronunciation       string      `json:"pronunciation"`
					RelatedWords        interface{} `json:"relatedWords"`
					SpeechPartID        interface{} `json:"speechPartId"`
					Transcription       string      `json:"transcription"`
					Translations        interface{} `json:"translations"`
					WordLemmaID         interface{} `json:"wordLemmaId"`
					WordLemmaValue      interface{} `json:"wordLemmaValue"`
					WordSets            []struct {
						CountWords int    `json:"countWords"`
						ID         int    `json:"id"`
						Name       string `json:"name"`
					} `json:"wordSets"`
					WordType  int    `json:"wordType"`
					WordValue string `json:"wordValue"`
				}
				for _, g := range pageResp.Data {
					if g.GroupName == groupName {
						words = g.Words
						break
					}
				}
				if len(words) == 0 {
					break
				}
				// Write TSV rows with third column: WordSet name (with global dedup by normalized wordValue)
				for _, w := range words {
					key := normalizeWord(w.WordValue)
					if key == "" {
						skippedEmpty++
						continue
					}
					if _, ok := seen[key]; ok {
						dupCount++
						continue
					}
					seen[key] = struct{}{}
					fmt.Fprintf(
						f,
						"%s\t%s\t%s\n",
						sanitizeTSV(w.WordValue),
						sanitizeTSV(w.CombinedTranslation),
						sanitizeTSV(setName),
					)
				}
				// Next offset from last word id
				last := words[len(words)-1]
				offset = map[string]int{"wordId": last.ID}
				time.Sleep(500 * time.Millisecond)
			}
		}
	}

	// Print deduplication summary
	fmt.Printf("duplicates removed: %d\n", dupCount)
	fmt.Printf("unique words: %d\n", len(seen))
}

func authenticate(ctx context.Context, client *http.Client) (string, error) {
	// Prepare body according to BodyAuth structure
	body := BodyAuth{
		Type: "email",
		Credentials: BodyAuthCredentials{
			Email:    email,
			Password: password,
		},
	}
	b, err := json.Marshal(body)
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, authURL, bytes.NewReader(b))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")

	res, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	io.Copy(io.Discard, res.Body)

	// Extract cookie named "remember"
	for _, c := range res.Cookies() {
		if c.Name == "remember" && c.Value != "" {
			return c.Value, nil
		}
	}
	return "", errors.New("remember cookie not found in auth response")
}

func getWords(ctx context.Context, client *http.Client, remember string) (*GetWordResponse, error) {
	// Build request payload
	var reqBody GetWordRequest
	reqBody.APIVersion = "1.0.1"
	reqBody.AttrList.Association = "as"
	reqBody.AttrList.CombinedTranslation = "trc"
	reqBody.AttrList.Created = "cd"
	reqBody.AttrList.ID = "id"
	reqBody.AttrList.LearningStatus = "ls"
	reqBody.AttrList.ListWordSets = "listWordSets"
	reqBody.AttrList.Origin = "wo"
	reqBody.AttrList.Picture = "pic"
	reqBody.AttrList.Progress = "pi"
	reqBody.AttrList.Pronunciation = "pron"
	reqBody.AttrList.RelatedWords = "rw"
	reqBody.AttrList.SpeechPartID = "pid"
	reqBody.AttrList.Trainings = "trainings"
	reqBody.AttrList.Transcription = "scr"
	reqBody.AttrList.Translations = "trs"
	reqBody.AttrList.WordLemmaID = "lid"
	reqBody.AttrList.WordLemmaValue = "lwd"
	reqBody.AttrList.WordSets = "ws"
	reqBody.AttrList.WordType = "wt"
	reqBody.AttrList.WordValue = "wd"
	reqBody.Category = ""
	reqBody.DateGroup = "start"
	reqBody.Mode = "basic"
	reqBody.Offset = nil
	reqBody.PerPage = 30
	reqBody.Search = ""
	reqBody.Status = ""
	reqBody.Training = nil
	reqBody.WordSetID = 1

	b, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, getWordsURL, bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if remember != "" {
		req.Header.Add("Cookie", fmt.Sprintf("remember=%s", remember))
	}

	res, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	bodyBytes, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}

	if res.StatusCode < 200 || res.StatusCode > 299 {
		return nil, fmt.Errorf("GetWords HTTP %d: %s", res.StatusCode, string(bodyBytes))
	}

	var parsed GetWordResponse
	if err := json.Unmarshal(bodyBytes, &parsed); err != nil {
		return nil, err
	}
	return &parsed, nil
}

// getWordsPage sends a GetWords request for a specific word set, date group and offset
func getWordsPage(ctx context.Context, client *http.Client, remember string, wordSetID int, dateGroup string, offset interface{}, perPage int) (*GetWordResponse, error) {
	// Build request payload
	var reqBody GetWordRequest
	reqBody.APIVersion = "1.0.1"
	reqBody.AttrList.Association = "as"
	reqBody.AttrList.CombinedTranslation = "trc"
	reqBody.AttrList.Created = "cd"
	reqBody.AttrList.ID = "id"
	reqBody.AttrList.LearningStatus = "ls"
	reqBody.AttrList.ListWordSets = "listWordSets"
	reqBody.AttrList.Origin = "wo"
	reqBody.AttrList.Picture = "pic"
	reqBody.AttrList.Progress = "pi"
	reqBody.AttrList.Pronunciation = "pron"
	reqBody.AttrList.RelatedWords = "rw"
	reqBody.AttrList.SpeechPartID = "pid"
	reqBody.AttrList.Trainings = "trainings"
	reqBody.AttrList.Transcription = "scr"
	reqBody.AttrList.Translations = "trs"
	reqBody.AttrList.WordLemmaID = "lid"
	reqBody.AttrList.WordLemmaValue = "lwd"
	reqBody.AttrList.WordSets = "ws"
	reqBody.AttrList.WordType = "wt"
	reqBody.AttrList.WordValue = "wd"
	reqBody.Category = ""
	reqBody.DateGroup = dateGroup
	reqBody.Mode = "basic"
	reqBody.Offset = offset
	reqBody.PerPage = perPage
	reqBody.Search = ""
	reqBody.Status = ""
	reqBody.Training = nil
	reqBody.WordSetID = wordSetID

	b, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, getWordsURL, bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	// Set remember cookie
	req.AddCookie(&http.Cookie{Name: "remember", Value: remember})

	res, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		bb, _ := io.ReadAll(res.Body)
		return nil, fmt.Errorf("GetWords HTTP %d: %s", res.StatusCode, string(bb))
	}

	var resp GetWordResponse
	if err := json.NewDecoder(res.Body).Decode(&resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func sanitizeTSV(s string) string {
	// replace tabs and newlines to keep TSV well-formed
	b := make([]rune, 0, len(s))
	for _, r := range s {
		switch r {
		case '\t', '\n', '\r':
			b = append(b, ' ')
		default:
			b = append(b, r)
		}
	}
	return string(b)
}

// normalizeWord applies global-dedup normalization rules:
// - trim spaces
// - lowercase
// - replace any non letter/digit with a single space
// - collapse consecutive spaces to one
// - remove leading/trailing spaces
func normalizeWord(s string) string {
	if s == "" {
		return ""
	}
	var b []rune
	b = make([]rune, 0, len(s))
	lastSpace := true // so leading spaces are skipped
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b = append(b, unicode.ToLower(r))
			lastSpace = false
		} else if unicode.IsSpace(r) || !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			if !lastSpace {
				b = append(b, ' ')
				lastSpace = true
			}
		}
	}
	// Trim trailing space if any
	if len(b) > 0 && b[len(b)-1] == ' ' {
		b = b[:len(b)-1]
	}
	// Convert to string and TrimSpace as extra safety
	out := string(b)
	out = strings.TrimSpace(out)
	return out
}
