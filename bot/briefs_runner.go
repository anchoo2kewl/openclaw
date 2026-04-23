package main

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// ---------- Execute dispatcher -----------------------------------------------

func executeBrief(ctx context.Context, b Brief) (string, error) {
	count := b.Config.Count
	if count <= 0 {
		count = 10
	}
	switch b.Type {
	case BriefHackerNews:
		return fetchHackerNews(ctx, count)
	case BriefWorldNews:
		return fetchWorldNews(ctx, count)
	case BriefWeather:
		loc := b.Config.Location
		if loc == "" {
			loc = "Mississauga,Ontario,Canada"
		}
		return fetchWeather(ctx, loc)
	case BriefStocks:
		return fetchStocks(ctx, b.Config.Tickers)
	default:
		return "", fmt.Errorf("unknown brief type: %s", b.Type)
	}
}

// ---------- Telegram delivery ------------------------------------------------

func sendBriefToTelegram(ctx context.Context, botToken, chatID, htmlMsg string) error {
	const maxLen = 4000
	chunks := splitMessage(htmlMsg, maxLen)
	for _, chunk := range chunks {
		if err := sendTelegramHTML(ctx, botToken, chatID, chunk); err != nil {
			return err
		}
	}
	return nil
}

func sendTelegramHTML(ctx context.Context, botToken, chatID, text string) error {
	apiURL := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", botToken)
	form := url.Values{
		"chat_id":                  {chatID},
		"text":                     {text},
		"parse_mode":               {"HTML"},
		"disable_web_page_preview": {"true"},
	}
	req, err := http.NewRequestWithContext(ctx, "POST", apiURL, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("telegram API %d: %s", resp.StatusCode, body)
	}
	return nil
}

func splitMessage(msg string, maxLen int) []string {
	if len(msg) <= maxLen {
		return []string{msg}
	}
	var parts []string
	for len(msg) > 0 {
		if len(msg) <= maxLen {
			parts = append(parts, msg)
			break
		}
		cut := strings.LastIndex(msg[:maxLen], "\n")
		if cut < maxLen/2 {
			cut = maxLen
		}
		parts = append(parts, msg[:cut])
		msg = msg[cut:]
	}
	return parts
}

// ---------- Hacker News ------------------------------------------------------

func fetchHackerNews(ctx context.Context, count int) (string, error) {
	var ids []int
	if err := httpGetJSON(ctx, "https://hacker-news.firebaseio.com/v0/topstories.json", &ids); err != nil {
		return "", fmt.Errorf("fetch HN top stories: %w", err)
	}
	if len(ids) > count {
		ids = ids[:count]
	}

	now := time.Now().UTC().Format("2006-01-02 15:04 UTC")
	var sb strings.Builder
	fmt.Fprintf(&sb, "📡 <b>Hacker News Top %d</b> — %s\n", count, now)

	for i, id := range ids {
		var item struct {
			Title       string `json:"title"`
			URL         string `json:"url"`
			Score       int    `json:"score"`
			Descendants int    `json:"descendants"`
			By          string `json:"by"`
			Time        int64  `json:"time"`
			ID          int    `json:"id"`
		}
		itemURL := fmt.Sprintf("https://hacker-news.firebaseio.com/v0/item/%d.json", id)
		if err := httpGetJSON(ctx, itemURL, &item); err != nil {
			continue
		}
		link := item.URL
		if link == "" {
			link = fmt.Sprintf("https://news.ycombinator.com/item?id=%d", item.ID)
		}
		hnLink := fmt.Sprintf("https://news.ycombinator.com/item?id=%d", item.ID)
		posted := time.Unix(item.Time, 0).UTC().Format("2006-01-02")

		fmt.Fprintf(&sb, "\n<b>%d.</b> <a href=\"%s\">%s</a>\n", i+1, link, html.EscapeString(item.Title))
		fmt.Fprintf(&sb, "    ⬆ %d pts · 💬 <a href=\"%s\">%d comments</a> · %s", item.Score, hnLink, item.Descendants, posted)
	}
	return sb.String(), nil
}

// ---------- World News (Google News RSS) -------------------------------------

func fetchWorldNews(ctx context.Context, count int) (string, error) {
	feedURL := "https://news.google.com/rss/topics/CAAqJggKIiBDQkFTRWdvSUwyMHZNRGx1YlY4U0FtVnVHZ0pWVXlnQVAB?hl=en-US&gl=US&ceid=US:en"
	req, err := http.NewRequestWithContext(ctx, "GET", feedURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var rss struct {
		Channel struct {
			Items []struct {
				Title   string `xml:"title"`
				Link    string `xml:"link"`
				PubDate string `xml:"pubDate"`
				Source  string `xml:"source"`
			} `xml:"item"`
		} `xml:"channel"`
	}
	if err := xml.NewDecoder(resp.Body).Decode(&rss); err != nil {
		return "", err
	}

	items := rss.Channel.Items
	if len(items) > count {
		items = items[:count]
	}

	now := time.Now().UTC().Format("2006-01-02 15:04 UTC")
	var sb strings.Builder
	fmt.Fprintf(&sb, "🌍 <b>World News Top %d</b> — %s\n", count, now)

	for i, item := range items {
		title := item.Title
		source := item.Source
		if idx := strings.LastIndex(title, " - "); idx > 0 && source != "" {
			title = title[:idx]
		}
		var datePart string
		if t, err := time.Parse("Mon, 02 Jan 2006 15:04:05 MST", item.PubDate); err == nil {
			datePart = " · " + t.Format("Jan 02")
		}
		srcPart := ""
		if source != "" {
			srcPart = " · <i>" + html.EscapeString(source) + "</i>"
		}
		fmt.Fprintf(&sb, "\n<b>%d.</b> <a href=\"%s\">%s</a>%s%s", i+1, item.Link, html.EscapeString(title), srcPart, datePart)
	}
	return sb.String(), nil
}

// ---------- Weather (wttr.in) ------------------------------------------------

func fetchWeather(ctx context.Context, location string) (string, error) {
	apiURL := fmt.Sprintf("https://wttr.in/%s?format=j1", url.PathEscape(location))
	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "curl/8.0")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var data struct {
		CurrentCondition []struct {
			TempC       string `json:"temp_C"`
			TempF       string `json:"temp_F"`
			FeelsLikeC  string `json:"FeelsLikeC"`
			FeelsLikeF  string `json:"FeelsLikeF"`
			WeatherDesc []struct {
				Value string `json:"value"`
			} `json:"weatherDesc"`
			Humidity    string `json:"humidity"`
			WindSpeed   string `json:"windspeedKmph"`
			WindDir     string `json:"winddir16Point"`
			PrecipMM    string `json:"precipMM"`
			UVIndex     string `json:"uvIndex"`
			Visibility  string `json:"visibility"`
		} `json:"current_condition"`
		NearestArea []struct {
			AreaName []struct{ Value string } `json:"areaName"`
			Region   []struct{ Value string } `json:"region"`
		} `json:"nearest_area"`
		Weather []struct {
			MaxTempC  string `json:"maxtempC"`
			MinTempC  string `json:"mintempC"`
			MaxTempF  string `json:"maxtempF"`
			MinTempF  string `json:"mintempF"`
			Date      string `json:"date"`
			Astronomy []struct {
				Sunrise string `json:"sunrise"`
				Sunset  string `json:"sunset"`
			} `json:"astronomy"`
		} `json:"weather"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return "", err
	}

	cur := data.CurrentCondition[0]
	city := location
	region := ""
	if len(data.NearestArea) > 0 {
		if len(data.NearestArea[0].AreaName) > 0 {
			city = data.NearestArea[0].AreaName[0].Value
		}
		if len(data.NearestArea[0].Region) > 0 {
			region = data.NearestArea[0].Region[0].Value
		}
	}
	desc := ""
	if len(cur.WeatherDesc) > 0 {
		desc = cur.WeatherDesc[0].Value
	}

	now := time.Now().UTC().Format("2006-01-02 15:04 UTC")
	var sb strings.Builder
	fmt.Fprintf(&sb, "🌤 <b>Weather — %s, %s</b>\n", city, region)
	fmt.Fprintf(&sb, "📍 %s · %s\n\n", location, now)
	fmt.Fprintf(&sb, "<b>%s</b>\n", desc)
	fmt.Fprintf(&sb, "🌡 <b>%s°C</b> (%s°F) · Feels like %s°C (%s°F)\n", cur.TempC, cur.TempF, cur.FeelsLikeC, cur.FeelsLikeF)
	fmt.Fprintf(&sb, "💧 Humidity: %s%% · Precip: %smm\n", cur.Humidity, cur.PrecipMM)
	fmt.Fprintf(&sb, "💨 Wind: %s km/h %s\n", cur.WindSpeed, cur.WindDir)
	fmt.Fprintf(&sb, "👁 Visibility: %s km · UV: %s", cur.Visibility, cur.UVIndex)

	if len(data.Weather) > 0 {
		today := data.Weather[0]
		fmt.Fprintf(&sb, "\n\n📅 <b>Today:</b> High %s°C (%s°F) · Low %s°C (%s°F)", today.MaxTempC, today.MaxTempF, today.MinTempC, today.MinTempF)
		if len(today.Astronomy) > 0 {
			fmt.Fprintf(&sb, "\n🌅 Sunrise %s · Sunset %s", today.Astronomy[0].Sunrise, today.Astronomy[0].Sunset)
		}
	}
	if len(data.Weather) > 1 {
		tmrw := data.Weather[1]
		fmt.Fprintf(&sb, "\n📅 <b>Tomorrow (%s):</b> %s°C / %s°C", tmrw.Date, tmrw.MaxTempC, tmrw.MinTempC)
	}
	return sb.String(), nil
}

// ---------- Stocks (Yahoo Finance) -------------------------------------------

func fetchStocks(ctx context.Context, tickers []TickerEntry) (string, error) {
	now := time.Now().UTC().Format("2006-01-02 15:04 UTC")
	var sb strings.Builder
	fmt.Fprintf(&sb, "📊 <b>Morning Market Brief</b> — %s\n", now)

	for _, t := range tickers {
		q, err := fetchYahooQuote(ctx, t.Symbol)
		if err != nil {
			fmt.Fprintf(&sb, "\n<b>%s</b> (%s): unavailable", t.Label, t.Symbol)
			continue
		}
		fmt.Fprintf(&sb, "\n<b>%s</b> (%s)", t.Label, t.Symbol)
		fmt.Fprintf(&sb, "\n  Last Close: %s", fmtPrice(q.PreviousClose, q.Currency))

		if q.PreMarketPrice > 0 {
			pct := pctChange(q.PreMarketPrice, q.PreviousClose)
			fmt.Fprintf(&sb, "\n  Pre-Market: %s%s", fmtPrice(q.PreMarketPrice, q.Currency), pct)
		} else if q.PostMarketPrice > 0 {
			pct := pctChange(q.PostMarketPrice, q.PreviousClose)
			fmt.Fprintf(&sb, "\n  After-Hours: %s%s", fmtPrice(q.PostMarketPrice, q.Currency), pct)
		} else if q.RegularPrice > 0 && q.RegularPrice != q.PreviousClose {
			pct := pctChange(q.RegularPrice, q.PreviousClose)
			fmt.Fprintf(&sb, "\n  Current: %s%s", fmtPrice(q.RegularPrice, q.Currency), pct)
		}
		sb.WriteString("\n")
	}
	return sb.String(), nil
}

type yahooQuote struct {
	RegularPrice   float64
	PreviousClose  float64
	PreMarketPrice float64
	PostMarketPrice float64
	Currency       string
}

func fetchYahooQuote(ctx context.Context, symbol string) (yahooQuote, error) {
	apiURL := fmt.Sprintf("https://query1.finance.yahoo.com/v8/finance/chart/%s?range=1d&interval=1d&includePrePost=true", url.PathEscape(symbol))
	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		return yahooQuote{}, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return yahooQuote{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return yahooQuote{}, fmt.Errorf("yahoo %d for %s", resp.StatusCode, symbol)
	}

	var data struct {
		Chart struct {
			Result []struct {
				Meta struct {
					RegularMarketPrice float64 `json:"regularMarketPrice"`
					PreviousClose      float64 `json:"previousClose"`
					ChartPreviousClose float64 `json:"chartPreviousClose"`
					PreMarketPrice     float64 `json:"preMarketPrice"`
					PostMarketPrice    float64 `json:"postMarketPrice"`
					Currency           string  `json:"currency"`
				} `json:"meta"`
			} `json:"result"`
		} `json:"chart"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return yahooQuote{}, err
	}
	if len(data.Chart.Result) == 0 {
		return yahooQuote{}, fmt.Errorf("no data for %s", symbol)
	}
	m := data.Chart.Result[0].Meta
	prev := m.PreviousClose
	if prev == 0 {
		prev = m.ChartPreviousClose
	}
	return yahooQuote{
		RegularPrice:    m.RegularMarketPrice,
		PreviousClose:   prev,
		PreMarketPrice:  m.PreMarketPrice,
		PostMarketPrice: m.PostMarketPrice,
		Currency:        m.Currency,
	}, nil
}

func fmtPrice(val float64, currency string) string {
	if val == 0 {
		return "—"
	}
	if currency == "" || currency == "USD" {
		return fmt.Sprintf("$%,.2f", val)
	}
	return fmt.Sprintf("%.2f %s", val, currency)
}

func pctChange(current, prev float64) string {
	if prev == 0 {
		return ""
	}
	change := ((current - prev) / prev) * 100
	arrow := "📈"
	sign := "+"
	if change < 0 {
		arrow = "📉"
		sign = ""
	}
	return fmt.Sprintf(" %s %s%.2f%%", arrow, sign, change)
}

// ---------- HTTP helper ------------------------------------------------------

func httpGetJSON(ctx context.Context, u string, dst any) error {
	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return json.NewDecoder(resp.Body).Decode(dst)
}
