package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/term"
)

// --- 颜色定义 ---
const (
	colorRed    = "\033[31m"
	colorGreen  = "\033[32m"
	colorYellow = "\033[33m"
	colorCyan   = "\033[36m"
	colorBeige  = "\033[38;5;180m"
	colorGray   = "\033[90m"
	colorReset  = "\033[0m"
	colorBold   = "\033[1m"
)

const (
	RemoteURL = "http://8.162.1.176:7890"
	LanURL    = "http://192.168.1.119:7890"
)

// --- 客户端逻辑 ---

type UnilinkClient struct {
	BaseURL  string
	Username string
	Client   *http.Client
	IsLan    bool
}

func NewUnilinkClient(baseURL, username string, isLan bool) *UnilinkClient {
	return &UnilinkClient{
		BaseURL:  baseURL,
		Username: username,
		IsLan:    isLan,
		Client:   &http.Client{Timeout: 5 * time.Second},
	}
}

func (c *UnilinkClient) handleLS() {
	mode := "公网"
	if c.IsLan { mode = "局域网" }
	fmt.Printf("%s[ 云端存储 - %s ]%s\n", colorYellow, mode, colorReset)
	
	files, err := c.GetCloudFiles()
	if err != nil {
		printError("无法读取云端列表，连接可能中断")
		return
	}
	if len(files) > 0 {
		for _, f := range files {
			fmt.Printf("  ☁  %s\n", f)
		}
	} else {
		fmt.Printf("  %s(暂无云端文件)%s\n", colorGray, colorReset)
	}

	wd, _ := os.Getwd()
	fmt.Printf("\n%s[ 本地目录: %s ]%s\n", colorBeige, wd, colorReset)
	localFiles, _ := os.ReadDir(".")
	for _, f := range localFiles {
		if f.IsDir() {
			fmt.Printf("  📁 %s/\n", f.Name())
		} else {
			fmt.Printf("  📄 %s\n", f.Name())
		}
	}
	fmt.Println()
}

func (c *UnilinkClient) GetCloudFiles() ([]string, error) {
	req, _ := http.NewRequest("GET", c.BaseURL+"/api/files", nil)
	req.Header.Set("X-Username", c.Username)
	resp, err := c.Client.Do(req)
	if err != nil { return nil, err }
	defer resp.Body.Close()
	var files []string
	json.NewDecoder(resp.Body).Decode(&files)
	return files, nil
}

func (c *UnilinkClient) handleCD(target string) {
	if target == "" { return }
	if err := os.Chdir(target); err != nil {
		printError("路径错误: " + err.Error())
	} else {
		wd, _ := os.Getwd()
		printSuccess("已切换到: " + wd)
	}
}

func (c *UnilinkClient) handleUpload(filename string) {
	file, err := os.Open(filename)
	if err != nil {
		printError("文件不存在")
		return
	}
	defer file.Close()
	
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, _ := writer.CreateFormFile("file", filepath.Base(filename))
	io.Copy(part, file)
	writer.Close()
	
	req, _ := http.NewRequest("POST", c.BaseURL+"/api/files/upload", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("X-Username", c.Username)
	
	resp, err := c.Client.Do(req)
	if err == nil && resp.StatusCode == 200 {
		printSuccess("文件 '" + filename + "' 上传成功")
	} else {
		printError("上传失败")
	}
}

func (c *UnilinkClient) handleDownload(filename string) {
	req, _ := http.NewRequest("GET", c.BaseURL+"/api/files/download/"+filename, nil)
	req.Header.Set("X-Username", c.Username)
	
	resp, err := c.Client.Do(req)
	if err == nil && resp.StatusCode == 200 {
		out, err := os.Create(filename)
		if err != nil {
			printError("创建本地文件失败")
			return
		}
		defer out.Close()
		io.Copy(out, resp.Body)
		printDownloadSuccess("文件 '" + filename + "' 下载成功")
	} else {
		printError("下载失败")
	}
}

func printError(msg string) { fmt.Printf("%s[!] %s%s\n", colorRed, msg, colorReset) }
func printSuccess(msg string) { fmt.Printf("%s[✓] %s%s\n", colorCyan, msg, colorReset) }
func printDownloadSuccess(msg string) { fmt.Printf("%s[✓] %s%s\n", colorGreen, msg, colorReset) }

func printBanner() {
	banner := []string{
		" ██     ██ ████     ██ ██ ██       ██ ████     ██ ██   ██",
		"░██    ░██░██░██   ░██░██░██      ░██░██░██   ░██░██  ██ ",
		"░██    ░██░██░░██  ░██░██░██      ░██░██░░██  ░██░██ ██  ",
		"░██    ░██░██ ░░██ ░██░██░██      ░██░██ ░░██ ░██░████   ",
		"░██    ░██░██  ░░██░██░██░██      ░██░██  ░░██░██░██░██  ",
		"░██    ░██░██   ░░████░██░██      ░██░██   ░░████░██░░██ ",
		"░░███████ ░██    ░░███░██░████████░██░██    ░░███░██ ░░██",
		" ░░░░░░░  ░░      ░░░ ░░ ░░░░░░░░ ░░ ░░      ░░░ ░░   ░░ ",
	}
	fmt.Print(colorBeige)
	for _, line := range banner {
		fmt.Println(line)
	}
	fmt.Print(colorReset)
}

func main() {
	if len(os.Args) < 2 {
		fmt.Println("用法: unilink <用户名>")
		os.Exit(1)
	}
	username := os.Args[1]

	fmt.Printf("%s%s ➔ 身份验证: %s%s%s\n", colorBold, colorCyan, colorBeige, username, colorReset)
	fmt.Printf("%s请输入访问密钥: %s", colorGray, colorReset)
	passBytes, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Println()
	if err != nil { os.Exit(1) }
	password := strings.TrimSpace(string(passBytes))

	var baseURL string
	var isLan bool
	
	fmt.Print(colorGray + "正在探测可用链路...")
	loginData := map[string]string{"username": username, "password": password}
	jsonBytes, _ := json.Marshal(loginData)
	
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Post(RemoteURL+"/login", "application/json", bytes.NewBuffer(jsonBytes))
	
	if err != nil {
		fmt.Println("\n" + colorYellow + "[!] 公网不可达，自动切换局域网(192.168.1.119)...")
		resp, err = client.Post(LanURL+"/login", "application/json", bytes.NewBuffer(jsonBytes))
		if err != nil {
			printError("网络链路故障，请检查连接")
			os.Exit(1)
		}
		baseURL = LanURL
		isLan = true
	} else if resp.StatusCode != 200 {
		printError("认证失败：密钥不正确")
		os.Exit(1)
	} else {
		baseURL = RemoteURL
		isLan = false
		fmt.Println(colorCyan + " [在线]")
	}
	if resp != nil { resp.Body.Close() }

	fmt.Print("\033[H\033[2J")
	printBanner()
	
	netMode := "公网模式"
	if isLan { netMode = "局域网模式" }
	fmt.Printf("%s[✓] 验证通过 | 当前链路: %s%s\n", colorCyan, colorBold+colorYellow, netMode+colorReset)
	fmt.Printf("%s输入 'switch' 切换网络 | 输入 'help' 查看指令清单%s\n", colorGray, colorReset)

	unilink := NewUnilinkClient(baseURL, username, isLan)
	reader := bufio.NewReader(os.Stdin)

	for {
		wd, _ := os.Getwd()
		tag := "NET"
		if unilink.IsLan { tag = "LAN" }
		fmt.Printf("%s[%s]%s %s[%s]%s %s> ", colorBeige, wd, colorReset, colorYellow, tag, colorReset, username)
		
		line, _ := reader.ReadString('\n')
		line = strings.TrimSpace(line)
		if line == "" { continue }
		parts := strings.SplitN(line, " ", 2)
		cmd := parts[0]
		arg := ""
		if len(parts) > 1 { arg = parts[1] }

		switch cmd {
		case "ls":
			unilink.handleLS()
		case "cd":
			unilink.handleCD(arg)
		case "upload":
			unilink.handleUpload(arg)
		case "download":
			unilink.handleDownload(arg)
		case "switch", "lan":
			if unilink.IsLan {
				unilink.BaseURL = RemoteURL
				unilink.IsLan = false
				printSuccess("已手动切换至: 公网模式")
			} else {
				unilink.BaseURL = LanURL
				unilink.IsLan = true
				printSuccess("已手动切换至: 局域网模式 (192.168.1.119)")
			}
		case "help":
			fmt.Println("\n可用指令清单:")
			fmt.Println("  ls              - 列出云端和本地文件")
			fmt.Println("  cd <目录>       - 切换本地电脑目录")
			fmt.Println("  upload <名>     - 上传本地文件到云端")
			fmt.Println("  download <名>   - 下载云端文件到本地")
			fmt.Println("  switch          - 在公网/局域网间手动切换")
			fmt.Println("  exit            - 安全退出程序\n")
		case "exit":
			fmt.Println("正在注销，再见！")
			return
		default:
			printError("未知指令: " + cmd)
		}
	}
}
