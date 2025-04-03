package main

import (
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"time"

	//"time" // 用于周计算

	"github.com/gin-gonic/gin"
	_ "github.com/go-sql-driver/mysql" // 注意下划线 "_" 匿名导入，仅执行驱动的init()函数进行注册
)

// 全局数据库连接变量 (更好的实践是将其封装在结构体中或通过依赖注入传递)
var db *sql.DB

// ---- 数据库模型 ----
type Task struct {
	ID             int64     `json:"id"`              // 任务ID (使用 int64 以防 ID 很大)
	Description    string    `json:"description"`     // 任务描述
	WeekIdentifier string    `json:"week_identifier"` // 周标识符
	Status         string    `json:"status"`          // 任务状态 ('pending', 'completed')
	CreatedAt      time.Time `json:"created_at"`      // 创建时间
	UpdatedAt      time.Time `json:"updated_at"`      // 更新时间
}

// ---- 工具函数 (稍后添加) ----
// 计算 ISO 8601 周标识符 (YYYY-WW)
func getCurrentWeekIdentifier() string {
	now := time.Now()
	year, week := now.ISOWeek()
	// 格式化为 "YYYY-WW"
	return fmt.Sprintf("%d-W%02d", year, week)
}

// ---- HTTP Handler 函数 ----

// 处理创建新任务的请求 (POST /tasks)
func createTaskHandler(c *gin.Context) {
	// 1. 定义一个临时的结构体来接收请求体中的数据
	//    我们只需要 description，week_identifier 和 status 会自动设置
	var requestBody struct {
		Description string `json:"description" binding:"required"` // description 是必需的
	}

	// 2. 绑定 JSON 请求体到 requestBody 结构体
	if err := c.ShouldBindJSON(&requestBody); err != nil {
		// 如果请求体无效或缺少 description，返回错误
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的请求: " + err.Error()})
		return
	}

	// 3. 获取当前周标识符
	weekID := getCurrentWeekIdentifier()
	status := "pending" // 新任务默认为 pending

	// 4. 准备 SQL 插入语句
	query := "INSERT INTO tasks (description, week_identifier, status) VALUES (?, ?, ?)"
	result, err := db.ExecContext(c.Request.Context(), query, requestBody.Description, weekID, status)
	if err != nil {
		log.Printf("创建任务时数据库错误: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "创建任务失败"})
		return
	}

	// 5. 获取新插入任务的 ID
	newID, err := result.LastInsertId()
	if err != nil {
		log.Printf("获取新任务 ID 失败: %v", err)
		// 即使无法获取 ID，任务也已创建，但最好通知客户端
		c.JSON(http.StatusInternalServerError, gin.H{"error": "任务已创建，但获取 ID 失败"})
		return
	}

	// 6. (可选) 可以查询刚创建的任务并返回，或者只返回 ID
	// 为了简单起见，我们只返回成功消息和 ID
	c.JSON(http.StatusCreated, gin.H{
		"message":         "任务创建成功",
		"task_id":         newID,
		"week_identifier": weekID,
		"description":     requestBody.Description,
		"status":          status,
	})
}

// 处理获取任务列表的请求 (GET /tasks)
// 可以通过查询参数 ?week=YYYY-WW 来指定周，否则默认为当前周
func getTasksHandler(c *gin.Context) {
	// 1. 获取查询参数 'week'
	weekID := c.Query("week")

	// 2. 如果 'week' 参数为空，则使用当前周
	if weekID == "" {
		weekID = getCurrentWeekIdentifier()
	} else {
		// 可选：验证 weekID 格式是否为 YYYY-WW
		// (可以使用正则表达式或简单的字符串检查)
		// 例如: matched, _ := regexp.MatchString(`^\d{4}-W(0[1-9]|[1-4]\d|5[0-3])$`, weekID)
		// if !matched { ... return bad request ... }
	}

	// 3. 准备 SQL 查询语句
	query := "SELECT id, description, week_identifier, status, created_at, updated_at FROM tasks WHERE week_identifier = ? ORDER BY created_at DESC"
	rows, err := db.QueryContext(c.Request.Context(), query, weekID)
	if err != nil {
		log.Printf("查询任务时数据库错误 (week: %s): %v", weekID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "获取任务列表失败"})
		return
	}
	defer rows.Close() // 确保查询结果集被关闭

	// 4. 遍历查询结果
	tasks := []Task{} // 创建一个 Task 切片来存储结果
	for rows.Next() {
		var task Task
		// 将每一行的数据扫描到 task 结构体变量中
		if err := rows.Scan(&task.ID, &task.Description, &task.WeekIdentifier, &task.Status, &task.CreatedAt, &task.UpdatedAt); err != nil {
			log.Printf("扫描任务行数据时出错: %v", err)
			// 可以选择继续处理其他行，或者直接返回错误
			c.JSON(http.StatusInternalServerError, gin.H{"error": "处理任务数据时出错"})
			return // 遇到错误，提前返回
		}
		tasks = append(tasks, task) // 将扫描到的任务添加到切片中
	}

	// 5. 检查遍历过程中是否发生错误
	if err = rows.Err(); err != nil {
		log.Printf("遍历任务结果集时出错: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "读取任务列表时出错"})
		return
	}

	// 6. 返回查询到的任务列表 (JSON 格式)
	// 如果没有任务，会返回一个空列表 []
	c.JSON(http.StatusOK, tasks)
}

// ... (其他 Handler 函数) ...

// ... (main 函数) ...

func main() {
	// ---- 1. 数据库连接 ----
	// 注意：请将 "user:password@tcp(127.0.0.1:3306)/database_name" 替换为你的实际数据库连接信息
	// 确保添加 ?parseTime=true 参数以正确处理 TIMESTAMP/DATETIME 类型
	dsn := "root:lx123456@tcp(127.0.0.1:3306)/WeeklyTracker?parseTime=true"
	var err error
	db, err = sql.Open("mysql", dsn)
	if err != nil {
		log.Fatalf("无法连接到数据库: %v", err)
	}
	defer db.Close() // 确保在程序退出时关闭连接

	// 检查数据库连接是否有效
	err = db.Ping()
	if err != nil {
		log.Fatalf("无法 Ping 数据库: %v", err)
	}
	fmt.Println("成功连接到数据库!")

	// ---- 2. 设置 Gin 路由 ----
	router := gin.Default()

	// 示例路由 (稍后会替换为实际的任务处理路由)
	router.GET("/", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "欢迎使用周任务跟踪器!"})
	})

	// 定义任务相关的路由组 (更好的组织方式)
	taskRoutes := router.Group("/tasks")
	{
		taskRoutes.POST("/", createTaskHandler) // 关联创建任务的 Handler
		taskRoutes.GET("/", getTasksHandler)    // 关联获取任务列表的 Handler
		// taskRoutes.PUT("/:id", updateTaskHandler)    // 稍后实现
		// taskRoutes.PATCH("/:id/status", updateTaskStatusHandler) // 稍后实现
		// taskRoutes.DELETE("/:id", deleteTaskHandler) // 稍后实现
	}

	// 定义看板路由
	// 定义看板路由
	// router.GET("/dashboard", getDashboardHandler) // 获取看板数据

	// ---- 3. 启动服务器 ----
	fmt.Println("服务器启动于 http://localhost:8080")
	if err := router.Run(":8080"); err != nil {
		log.Fatalf("无法启动服务器: %v", err)
	}
}
