package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	_ "github.com/go-sql-driver/mysql"
)

// --- Configuration ---
const (
	Port        = ":7890"
	DBUser      = "root"
	DBPass      = "43g3hqweg43q"
	DBName      = "unilink"
	BaseStorage = "./storage"
	AdminToken  = "unilink_admin_2026" // 管理后台的简易 Token
)

var db *sql.DB

// --- Models ---
type User struct {
	ID          int64  `json:"id"`
	Username    string `json:"username"`
	Password    string `json:"password"`
	StoragePath string `json:"storage_path"`
	QuotaBytes  int64  `json:"quota_bytes"` // 存储配额（字节）
	UsedBytes   int64  `json:"used_bytes"`  // 实时占用（不存储在 DB，动态计算）
}

// --- Utility Functions ---

func getDirSize(path string) (int64, error) {
	var size int64
	err := filepath.Walk(path, func(_ string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			size += info.Size()
		}
		return nil
	})
	return size, err
}

func getUsername(r *http.Request) string {
	return r.Header.Get("X-Username")
}

func respondJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func respondError(w http.ResponseWriter, status int, message string) {
	w.WriteHeader(status)
	w.Write([]byte(message))
}

// --- User Handlers ---

func handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		respondError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "Invalid request")
		return
	}

	var user User
	err := db.QueryRow("SELECT id, username, password, quota_bytes FROM user WHERE username = ? AND password = ?",
		req.Username, req.Password).Scan(&user.ID, &user.Username, &user.Password, &user.QuotaBytes)

	if err != nil {
		respondError(w, http.StatusUnauthorized, "Login failed: Invalid credentials")
		return
	}

	respondJSON(w, http.StatusOK, map[string]interface{}{"message": "Login successful", "username": user.Username})
}

func handleListFiles(w http.ResponseWriter, r *http.Request) {
	username := getUsername(r)
	if username == "" {
		respondError(w, http.StatusUnauthorized, "Missing X-Username header")
		return
	}

	userDir := filepath.Join(BaseStorage, username)
	files, err := os.ReadDir(userDir)
	if err != nil {
		respondJSON(w, http.StatusOK, []string{})
		return
	}

	var fileNames []string
	for _, f := range files {
		if !f.IsDir() {
			fileNames = append(fileNames, f.Name())
		}
	}
	respondJSON(w, http.StatusOK, fileNames)
}

func handleUpload(w http.ResponseWriter, r *http.Request) {
	username := getUsername(r)
	if username == "" {
		respondError(w, http.StatusUnauthorized, "Missing X-Username header")
		return
	}

	// 1. 获取用户配额
	var quotaBytes int64
	err := db.QueryRow("SELECT quota_bytes FROM user WHERE username = ?", username).Scan(&quotaBytes)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "User not found")
		return
	}

	// 2. 检查当前沙盒占用
	userDir := filepath.Join(BaseStorage, username)
	os.MkdirAll(userDir, 0755)
	currentSize, _ := getDirSize(userDir)

	// 3. 处理上传
	r.ParseMultipartForm(100 << 20) // Max 100MB memory for multipart
	file, header, err := r.FormFile("file")
	if err != nil {
		respondError(w, http.StatusBadRequest, "Failed to get file")
		return
	}
	defer file.Close()

	// 4. 预测新文件大小是否超标
	if currentSize+header.Size > quotaBytes {
		errMsg := fmt.Sprintf("Quota exceeded! Limit: %.2f MB, Current: %.2f MB, New File: %.2f MB",
			float64(quotaBytes)/1024/1024, float64(currentSize)/1024/1024, float64(header.Size)/1024/1024)
		respondError(w, http.StatusForbidden, errMsg)
		return
	}

	dst, err := os.Create(filepath.Join(userDir, header.Filename))
	if err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to create file")
		return
	}
	defer dst.Close()

	if _, err := io.Copy(dst, file); err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to save file")
		return
	}

	fmt.Printf("[INFO] User %s uploaded %s (Size: %d)\n", username, header.Filename, header.Size)
	respondJSON(w, http.StatusOK, map[string]string{"message": "File uploaded successfully"})
}

func handleDownload(w http.ResponseWriter, r *http.Request) {
	username := getUsername(r)
	if username == "" {
		respondError(w, http.StatusUnauthorized, "Missing X-Username header")
		return
	}

	parts := strings.Split(r.URL.Path, "/")
	filename := parts[len(parts)-1]
	filePath := filepath.Join(BaseStorage, username, filename)

	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		respondError(w, http.StatusNotFound, "File not found")
		return
	}

	w.Header().Set("Content-Disposition", "attachment; filename="+filename)
	http.ServeFile(w, r, filePath)
}

// --- Admin Handlers ---

func handleAdminUsers(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Admin-Token") != AdminToken {
		respondError(w, http.StatusUnauthorized, "Invalid Admin Token")
		return
	}

	if r.Method == http.MethodGet {
		rows, err := db.Query("SELECT id, username, quota_bytes FROM user")
		if err != nil {
			respondError(w, http.StatusInternalServerError, "数据库查询失败: "+err.Error())
			return
		}
		defer rows.Close()

		var users []User
		for rows.Next() {
			var u User
			rows.Scan(&u.ID, &u.Username, &u.QuotaBytes)
			u.UsedBytes, _ = getDirSize(filepath.Join(BaseStorage, u.Username))
			users = append(users, u)
		}
		respondJSON(w, http.StatusOK, users)

	} else if r.Method == http.MethodPost {
		var req struct {
			Username string `json:"username"`
			Password string `json:"password"`
			QuotaMB  int64  `json:"quota_mb"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			respondError(w, http.StatusBadRequest, "无效的请求数据")
			return
		}

		if req.Username == "" || req.Password == "" {
			respondError(w, http.StatusBadRequest, "用户名和密码不能为空")
			return
		}

		quotaBytes := req.QuotaMB * 1024 * 1024

		_, err := db.Exec("INSERT INTO user (username, password, storage_path, quota_bytes) VALUES (?, ?, ?, ?)",
			req.Username, req.Password, req.Username, quotaBytes)
		if err != nil {
			emsg := err.Error()
			if strings.Contains(strings.ToLower(emsg), "duplicate") || strings.Contains(strings.ToLower(emsg), "unique") {
				respondError(w, http.StatusBadRequest, "该用户名已存在，请更换")
				return
			}
			respondError(w, http.StatusInternalServerError, "数据库写入错误: "+emsg)
			return
		}
		os.MkdirAll(filepath.Join(BaseStorage, req.Username), 0755)
		respondJSON(w, http.StatusOK, map[string]string{"message": "用户创建成功"})
	}
}

func handleAdminUpdateQuota(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Admin-Token") != AdminToken || r.Method != http.MethodPut {
		respondError(w, http.StatusUnauthorized, "未授权访问")
		return
	}
	var req struct {
		ID      int64 `json:"id"`
		QuotaMB int64 `json:"quota_mb"`
	}
	json.NewDecoder(r.Body).Decode(&req)
	_, err := db.Exec("UPDATE user SET quota_bytes = ? WHERE id = ?", req.QuotaMB*1024*1024, req.ID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "更新失败: "+err.Error())
		return
	}
	respondJSON(w, http.StatusOK, map[string]string{"message": "配额更新成功"})
}

// --- Admin UI (Embedded) ---
func handleAdminIndex(w http.ResponseWriter, r *http.Request) {
	html := `
<!DOCTYPE html>
<html>
<head>
    <title>Unilink 管理后台</title>
    <style>
        body { font-family: 'Microsoft YaHei', sans-serif; background: #1a1a1a; color: #eee; margin: 40px; }
        .card { background: #2d2d2d; padding: 20px; border-radius: 8px; box-shadow: 0 4px 10px rgba(0,0,0,0.3); }
        h1 { color: #f1c40f; }
        table { width: 100%; border-collapse: collapse; margin-top: 20px; }
        th, td { text-align: left; padding: 12px; border-bottom: 1px solid #444; }
        th { color: #f1c40f; }
        input, button { padding: 8px; border-radius: 4px; border: 1px solid #444; background: #3d3d3d; color: white; }
        button { background: #f1c40f; color: black; font-weight: bold; cursor: pointer; }
        button:hover { background: #d4ac0d; }
        .form-group { margin-bottom: 15px; }
    </style>
</head>
<body>
    <div class="card">
        <h1>Unilink 用户管理</h1>
        <div id="create-user">
            <h3>添加新用户</h3>
            <input type="text" id="new-user" placeholder="用户名">
            <input type="password" id="new-pass" placeholder="密码">
            <input type="number" id="new-quota" placeholder="配额 (MB)" value="100">
            <button onclick="addUser()">立即创建</button>
        </div>
        <hr style="border:0.5px solid #444; margin: 20px 0;">
        <table id="user-table">
            <thead>
                <tr>
                    <th>ID</th>
                    <th>用户名</th>
                    <th>已用空间</th>
                    <th>总配额 (MB)</th>
                    <th>操作</th>
                </tr>
            </thead>
            <tbody id="user-list"></tbody>
        </table>
    </div>

    <script>
        const token = "unilink_admin_2026";
        async function loadUsers() {
            try {
                const res = await fetch('/admin/api/users', { headers: {'Admin-Token': token} });
                const users = await res.json();
                const list = document.getElementById('user-list');
                list.innerHTML = '';
                users.forEach(u => {
                    const usedMB = (u.used_bytes / 1024 / 1024).toFixed(2);
                    const totalMB = (u.quota_bytes / 1024 / 1024).toFixed(0);
                    list.innerHTML += ` + "`" + `
                        <tr>
                            <td>${u.id}</td>
                            <td>${u.username}</td>
                            <td>${usedMB} MB</td>
                            <td><input type="number" value="${totalMB}" id="q-${u.id}" style="width:60px"> MB</td>
                            <td><button onclick="updateQuota(${u.id})">保存配额</button></td>
                        </tr>` + "`" + `;
                });
            } catch (e) {
                console.error("加载列表失败", e);
            }
        }

        async function addUser() {
            const user = document.getElementById('new-user').value;
            const pass = document.getElementById('new-pass').value;
            const quota = parseInt(document.getElementById('new-quota').value);
            
            const res = await fetch('/admin/api/users', {
                method: 'POST',
                headers: {'Admin-Token': token, 'Content-Type': 'application/json'},
                body: JSON.stringify({username: user, password: pass, quota_mb: quota})
            });
            
            if(res.ok) {
                alert('恭喜：用户创建成功！');
                document.getElementById('new-user').value = '';
                document.getElementById('new-pass').value = '';
                loadUsers();
            } else {
                const msg = await res.text();
                alert('创建失败：' + msg);
            }
        }

        async function updateQuota(id) {
            const quota = parseInt(document.getElementById('q-'+id).value);
            const res = await fetch('/admin/api/update_quota', {
                method: 'PUT',
                headers: {'Admin-Token': token, 'Content-Type': 'application/json'},
                body: JSON.stringify({id: id, quota_mb: quota})
            });
            if(res.ok) {
                alert('更新成功：配额已调整。');
                loadUsers();
            } else {
                const msg = await res.text();
                alert('更新失败：' + msg);
            }
        }

        loadUsers();
    </script>
</body>
</html>
`
	w.Header().Set("Content-Type", "text/html")
	w.Write([]byte(html))
}

func main() {
	banner := `
 ██     ██ ████     ██ ██ ██       ██ ████     ██ ██   ██
░██    ░██░██░██   ░██░██░██      ░██░██░██   ░██░██  ██ 
░██    ░██░██░░██  ░██░██░██      ░██░██░░██  ░██░██ ██  
░██    ░██░██ ░░██ ░██░██░██      ░██░██ ░░██ ░██░████   
░██    ░██░██  ░░██░██░██░██      ░██░██  ░░██░██░██░██  
░██    ░██░██   ░░████░██░██      ░██░██   ░░████░██░░██ 
░░███████ ░██    ░░███░██░████████░██░██    ░░███░██ ░░██
 ░░░░░░░  ░░      ░░░ ░░ ░░░░░░░░ ░░ ░░      ░░░ ░░   ░░ 
`
	fmt.Printf("\033[33m%s\033[0m\n", banner)

	// 1. DB Connection
	dsn := fmt.Sprintf("%s:%s@tcp(localhost:3306)/%s?charset=utf8mb4&parseTime=True&loc=Local", DBUser, DBPass, DBName)
	var err error
	db, err = sql.Open("mysql", dsn)
	if err != nil {
		log.Fatal(err)
	}
	if err := db.Ping(); err != nil {
		log.Fatalf("Database connection failed: %v", err)
	}

	// 2. Routes
	http.HandleFunc("/login", handleLogin)
	http.HandleFunc("/api/files", handleListFiles)
	http.HandleFunc("/api/files/upload", handleUpload)
	http.HandleFunc("/api/files/download/", handleDownload)

	// Admin Routes
	http.HandleFunc("/admin", handleAdminIndex)
	http.HandleFunc("/admin/api/users", handleAdminUsers)
	http.HandleFunc("/admin/api/update_quota", handleAdminUpdateQuota)

	os.MkdirAll(BaseStorage, 0755)
	fmt.Printf("[READY] Unilink Admin & Backend started on http://0.0.0.0%s\n", Port)
	fmt.Printf("[INFO] Admin URL: http://your-server-ip%s/admin\n", Port)
	log.Fatal(http.ListenAndServe(Port, nil))
}
