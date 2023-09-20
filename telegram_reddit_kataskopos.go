package reddit_kataskopos

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io/ioutil"
	"log"
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/GoogleCloudPlatform/functions-framework-go/functions"
)

const (
	searchCommand    string = "/search"
	telegramTokenEnv string = "GITHUB_BOT_TOKEN"
	postsLen         int    = 4
)

const templ = `
  Title: {{.Title}}
  Link: {{.Link}}
  ⭐: {{.Ups}}
`

type FirstJSONLevel struct {
	Data SecondJSONLevel `json:"data"`
}

type SecondJSONLevel struct {
	Children []ThirdJSONLevel `json:"children"`
}

type ThirdJSONLevel struct {
	Data FinalJSONLevel `json:"data"`
}

type FinalJSONLevel struct {
	Ups     int     `json:"ups"`
	Title   string  `json:"title"`
	Link    string  `json:"permalink"`
	Created float64 `json:"created"`
}

type Post struct {
	Ups   int
	Title string
	Link  string
}

type Update struct {
	UpdateID int     `json:"update_id"`
	Message  Message `json:"message"`
}

type Message struct {
	Text string `json:"text"`
	Chat Chat   `json:"chat"`
}

type Chat struct {
	Id int `json:"id"`
}

func init() {
	functions.HTTP("HandleTelegramWebhook", HandleTelegramWebhook)
}

func HandleTelegramWebhook(w http.ResponseWriter, r *http.Request) {
	var update, err = parseTelegramRequest(r)
	if err != nil {
		fmt.Printf("error parsing update, %s", err.Error())
		return
	}
	switch {
	case strings.HasPrefix(update.Message.Text, searchCommand):
		sanitizedString, err := sanitize(update.Message.Text, searchCommand)
		if err != nil {
			sendTextToTelegramChat(update.Message.Chat.Id, err.Error())
			fmt.Fprintf(w, "invald input")
			return
		}

		responseFunc, err := getPosts(sanitizedString, update.Message.Chat.Id)
		if err != nil {
			sendTextToTelegramChat(update.Message.Chat.Id, err.Error())
			fmt.Fprintf(w, "invalid input")
			return
		}
		fmt.Printf("successfully distributed to chat id %d, response from loop: %s", update.Message.Chat.Id, responseFunc)
		return

	default:
		fmt.Println("invalid command")
		return
	}

}

func parseTelegramRequest(r *http.Request) (*Update, error) {
	var update Update
	if err := json.NewDecoder(r.Body).Decode(&update); err != nil {
		fmt.Printf("could not decode incoming update %s", err.Error())
		return nil, err
	}
	return &update, nil
}

func sanitize(s, botCommand string) (string, error) {
	var lenBotCommand int = len(botCommand)
	if len(s) >= lenBotCommand {
		if s[:lenBotCommand] == botCommand {
			s = s[lenBotCommand:]
			s = strings.TrimSpace(s)
			fmt.Printf("type of value entered: %T\n", s)
		}
	} else {
		return "", errors.New("invalid value: you must enter /search {languague}")
	}
	return s, nil

}

func getPosts(subreddit string, chatId int) (string, error) {
	currentTime := time.Now()
	lastSevenDays := currentTime.AddDate(0, 0, -7)

	jsonResponse, err := makeRequest(subreddit)
	if err != nil {
		log.Printf("error: %s", err.Error())
		return "", err
	}
	slicePosts := parseJson(jsonResponse, lastSevenDays, currentTime)
	_, err = shufflePostsAndSend(slicePosts, chatId)
	if err != nil {
		return "", err
	}
	return "success", nil

}

func makeRequest(subreddit string) (FirstJSONLevel, error) {
	var jsonResponse FirstJSONLevel
	client := &http.Client{}
	subreddit_url := fmt.Sprintf("https://old.reddit.com/r/%s/.json?limit=100", subreddit)
	req, err := http.NewRequest("GET", subreddit_url, nil)

	if err != nil {
		log.Printf("error: %s", err.Error())
		return jsonResponse, err
	}

	req.Header.Set("User-Agent", "bla")

	resp, err := client.Do(req)
	if err != nil {
		log.Printf("error: %s", err.Error())
		return jsonResponse, err
	}

	body, err := ioutil.ReadAll(resp.Body)

	defer resp.Body.Close()

	err = json.Unmarshal(body, &jsonResponse)
	if err != nil {
		log.Printf("error: %s", err.Error())
		return jsonResponse, err
	}
	return jsonResponse, nil

}

func parseJson(jsonResponse FirstJSONLevel, lastSevenDays, currentTime time.Time) []Post {
	var postsArray []Post

	for i := range jsonResponse.Data.Children {
		postScore := jsonResponse.Data.Children[i].Data.Ups
		createdDateUnix := jsonResponse.Data.Children[i].Data.Created
		createdDate := time.Time(time.Unix(int64(createdDateUnix), 0))
		if postScore >= 50 && inTimeSpan(lastSevenDays, currentTime, createdDate) {
			fmt.Println(createdDate)

			jsonResponse.Data.Children[i].Data.Link = "https://reddit.com" + jsonResponse.Data.Children[i].Data.Link

			post := Post{Ups: jsonResponse.Data.Children[i].Data.Ups,
				Title: jsonResponse.Data.Children[i].Data.Title,
				Link:  jsonResponse.Data.Children[i].Data.Link,
			}
			postsArray = append(postsArray, post)
		}
	}
	return postsArray
}

func inTimeSpan(lastSevenDays, currentTime, check time.Time) bool {
	return check.After(lastSevenDays) && check.Before(currentTime)
}

func shufflePostsAndSend(postsArray []Post, chatId int) (string, error) {
	// shuffle data
	rand.Seed(time.Now().UnixNano())
	rand.Shuffle(len(postsArray), func(i, j int) { postsArray[i], postsArray[j] = postsArray[j], postsArray[i] })

	newSlice := make([]string, 0)

	for i := 1; i <= postsLen; i++ {
		post := postsArray[i]
		var report = template.Must(template.New("subrredits").Parse(templ))
		buf := &bytes.Buffer{}
		if err := report.Execute(buf, post); err != nil {
			sendTextToTelegramChat(chatId, err.Error())
			return "", err
		}
		s := buf.String()
		newSlice = append(newSlice, s)
	}
	textPosts := strings.Join(newSlice, "\n-------------\n")
	sendTextToTelegramChat(chatId, textPosts)
	return "finished", nil
}

func sendTextToTelegramChat(chatId int, text string) (string, error) {
	fmt.Printf("sending %s to chat_id: %d", text, chatId)

	var telegramApi string = "https://api.telegram.org/bot" + os.Getenv("GITHUB_BOT_TOKEN") + "/sendMessage"

	response, err := http.PostForm(
		telegramApi,
		url.Values{
			"chat_id": {strconv.Itoa(chatId)},
			"text":    {text},
		})
	if err != nil {
		fmt.Printf("error when posting text to the chat: %s", err.Error())
		return "", err
	}
	defer response.Body.Close()
	var bodyBytes, errRead = ioutil.ReadAll(response.Body)
	if errRead != nil {
		fmt.Printf("error parsing telegram answer %s", errRead.Error())
		return "", err
	}

	bodyString := string(bodyBytes)
	fmt.Printf("body of telegram response: %s", bodyString)
	return bodyString, nil

}
