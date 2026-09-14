package main

// 规则校验器命令行入口：
//
//	go run ./cmd/rulecheck             # 检查最近 5 局
//	go run ./cmd/rulecheck -n 10       # 检查最近 10 局
//	go run ./cmd/rulecheck <gameID>    # 检查指定的某几局
import (
	"fmt"
	"os"
	"strconv"

	"leve_up/models"

	"github.com/joho/godotenv"
)

func main() {
	for _, p := range []string{".env", "backend/.env", "../.env"} {
		if err := godotenv.Load(p); err == nil {
			break
		}
	}
	if err := models.InitDB(); err != nil {
		fmt.Fprintf(os.Stderr, "连数据库失败: %v\n", err)
		os.Exit(1)
	}

	// 先跑一遍与对局无关的静态规则（分值、主牌等级序列）
	if sv := models.CheckStaticRules(); len(sv) > 0 {
		fmt.Printf("=== 静态规则检查：%d 处问题 ===\n", len(sv))
		for _, v := range sv {
			fmt.Printf("  [%s] %s\n", v.Kind, v.Detail)
		}
	} else {
		fmt.Println("=== 静态规则检查：✓ 分值与主牌等级序列都对 ===")
	}

	// 对局 ID 本身也是一长串数字，不能靠"能不能转成数字"来区分，
	// 用 -n 显式表示"查最近几局"。
	limit := 5
	var gameIDs []string
	args := os.Args[1:]
	for i := 0; i < len(args); i++ {
		if args[i] == "-n" && i+1 < len(args) {
			n, err := strconv.Atoi(args[i+1])
			if err != nil || n <= 0 {
				fmt.Fprintf(os.Stderr, "-n 后面要跟正整数，收到 %q\n", args[i+1])
				os.Exit(2)
			}
			limit = n
			i++
			continue
		}
		gameIDs = append(gameIDs, args[i])
	}

	if len(gameIDs) == 0 {
		ids, err := models.ListRecentPlayedGames(limit)
		if err != nil {
			fmt.Fprintf(os.Stderr, "取对局列表失败: %v\n", err)
			os.Exit(1)
		}
		gameIDs = ids
	}

	if len(gameIDs) == 0 {
		fmt.Println("没有可检查的对局（需要有 playing_start 快照）")
		return
	}

	totalBad := 0
	for _, id := range gameIDs {
		res, err := models.CheckGameRules(id)
		if err != nil {
			fmt.Printf("\n=== %s ===\n  跳过: %v\n", id, err)
			continue
		}
		fmt.Printf("\n=== %s ===\n", id)
		fmt.Printf("  主花色=%s 级牌=%s 庄家=座位%d | %d 墩 / %d 手 / 场上 %d 分\n",
			res.TrumpSuit, res.TrumpRank, res.DealerSeat, res.Tricks, res.Plays, res.TotalPoint)
		sv, serr := models.CheckGameSettlement(id)
		if serr == nil && len(sv) > 0 {
			res.Violations = append(res.Violations, sv...)
		}
		if len(res.Violations) == 0 {
			fmt.Println("  ✓ 没发现规则问题")
			continue
		}
		totalBad += len(res.Violations)
		byKind := map[string]int{}
		for _, v := range res.Violations {
			byKind[v.Kind]++
		}
		fmt.Printf("  ✗ %d 处问题:\n", len(res.Violations))
		for kind, n := range byKind {
			fmt.Printf("     %s × %d\n", kind, n)
		}
		shown := 0
		for _, v := range res.Violations {
			if shown >= 15 {
				fmt.Printf("     …还有 %d 条未显示\n", len(res.Violations)-shown)
				break
			}
			fmt.Printf("     %s\n", v)
			shown++
		}
	}

	fmt.Printf("\n共检查 %d 局，发现 %d 处问题\n", len(gameIDs), totalBad)
	if totalBad > 0 {
		os.Exit(1)
	}
}
