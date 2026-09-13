#!/usr/bin/env python3
"""
多人模式模拟脚本:注册 5 个账号进入同一房间,完整打完一整局。

流程:
  1. 注册/登录 5 个账号
  2. 账号1 建房(座位1、房主),账号2-5 加入
  3. 5 人全部 ready(最后一个 ready 自动开局)
  4. 循环 deal-next 发牌直到发完
  5. 叫庄:挑级牌最多的账号亮庄,其余 pass,轮询等倒计时定庄
  6. 庄家扣底 7 张 + 叫朋友
  7. 出牌:座位1 由脚本按跟牌规则选牌,其余座位用 /ai-play 自动出
  8. 打到 finished,打印得分结果

用法:
  python3 simulate_multiplayer.py                 # 全速跑完一局
  python3 simulate_multiplayer.py --watch         # 观战模式:固定账号 bot_p1~p5,
                                                  # 开局前暂停等你在浏览器登录观看
  python3 simulate_multiplayer.py --watch --delay 3   # 自定义每手延时
"""

import argparse
import itertools
import json
import sys
import time
import urllib.error
import urllib.request

BASE = "http://localhost:8080"

VALUE_ORDER = {
    "2": 2, "3": 3, "4": 4, "5": 5, "6": 6, "7": 7, "8": 8, "9": 9,
    "10": 10, "J": 11, "Q": 12, "K": 13, "A": 14, "small": 15, "big": 16,
}


def api(method, path, token=None, body=None):
    """返回 (http_status, json_dict)。"""
    url = BASE + path
    data = json.dumps(body).encode() if body is not None else None
    req = urllib.request.Request(url, data=data, method=method)
    req.add_header("Content-Type", "application/json")
    if token:
        req.add_header("Authorization", "Bearer " + token)
    try:
        with urllib.request.urlopen(req, timeout=15) as resp:
            return resp.status, json.loads(resp.read().decode())
    except urllib.error.HTTPError as e:
        try:
            return e.code, json.loads(e.read().decode())
        except Exception:
            return e.code, {"error": str(e)}


def must(status, data, what):
    if status >= 400 or not data.get("success", True):
        print(f"[FATAL] {what} 失败: HTTP {status} {data}")
        sys.exit(1)
    return data


class Player:
    def __init__(self, name, token):
        self.name = name
        self.token = token
        self.seat = None  # myPosition

    def table(self, game_id):
        status, data = api("GET", f"/api/game/{game_id}/table", self.token)
        must(status, data, f"{self.name} 获取牌桌")
        return data["game"]


def register_players(n=5, fixed=False):
    players = []
    suffix = "" if fixed else str(int(time.time()))[-6:] + "_"
    for i in range(1, n + 1):
        name = f"bot_{suffix}p{i}" if not fixed else f"bot_p{i}"
        status, data = api("POST", "/api/register",
                           body={"username": name, "password": "pass1234"})
        if status == 409:
            status, data = api("POST", "/api/login",
                               body={"username": name, "password": "pass1234"})
        must(status, data, f"注册/登录 {name}")
        players.append(Player(name, data["token"]))
        print(f"[OK] 账号 {name} 就绪")
    return players


def is_trump(card, trump_suit, trump_rank):
    return (card.get("type") == "joker"
            or card.get("value") == trump_rank
            or (trump_suit and card.get("suit") == trump_suit))


def card_str(c):
    return f"{c.get('suit','?')[:1].upper()}{c.get('value','?')}"


def find_caller(players, game_id, trump_rank):
    """挑选级牌同花色张数最多的账号,返回 (player, indices)。"""
    best = (None, [])
    for p in players:
        g = p.table(game_id)
        hand = g.get("myHand") or []
        by_suit = {}
        for i, c in enumerate(hand):
            if c.get("value") == trump_rank and c.get("type") != "joker":
                by_suit.setdefault(c["suit"], []).append(i)
        for suit, idxs in by_suit.items():
            if len(idxs) > len(best[1]):
                best = (p, idxs[:3])
    return best


def pick_discard(hand, trump_suit, trump_rank):
    """扣底:优先扣非主、非分(5/10/K)的小牌,凑满 7 张。"""
    def score(ic):
        i, c = ic
        s = VALUE_ORDER.get(c.get("value"), 0)
        if is_trump(c, trump_suit, trump_rank):
            s += 100
        if c.get("value") in ("5", "10", "K"):
            s += 50
        return s
    ranked = sorted(enumerate(hand), key=score)
    return sorted(i for i, _ in ranked[:7])


def follow_candidates(hand, lead_cards, trump_suit, trump_rank):
    """返回 (同类牌索引列表, 其余牌索引列表),按牌值升序。"""
    lead = lead_cards[0]
    lead_is_trump = is_trump(lead, trump_suit, trump_rank)
    same, rest = [], []
    for i, c in enumerate(hand):
        c_trump = is_trump(c, trump_suit, trump_rank)
        if lead_is_trump:
            (same if c_trump else rest).append(i)
        else:
            if not c_trump and c.get("suit") == lead.get("suit"):
                same.append(i)
            else:
                rest.append(i)
    key = lambda i: VALUE_ORDER.get(hand[i].get("value"), 0)
    return sorted(same, key=key), sorted(rest, key=key)


def follow_attempts(hand, lead_cards, trump_suit, trump_rank, limit=300):
    """生成跟牌的候选索引组合(依次尝试直到服务端接受)。"""
    n = len(lead_cards)
    same, rest = follow_candidates(hand, lead_cards, trump_suit, trump_rank)
    attempts = []

    # 启发式:优先凑对子(领出为偶数张时),再补小牌
    # 注意:对子必须同花色+同点数(级牌 C2/D2 虽都是主但不成对)
    if n >= 2 and len(same) >= 2:
        by_key = {}
        for i in same:
            key = (hand[i].get("suit"), hand[i].get("value"))
            by_key.setdefault(key, []).append(i)
        pairs = [idxs for idxs in by_key.values() if len(idxs) >= 2]
        pick = []
        for idxs in sorted(pairs, key=lambda x: VALUE_ORDER.get(hand[x[0]].get("value"), 0)):
            while len(idxs) >= 2 and len(pick) + 2 <= n:
                pick += [idxs.pop(0), idxs.pop(0)]
        for i in same:
            if len(pick) >= n:
                break
            if i not in pick:
                pick.append(i)
        for i in rest:
            if len(pick) >= n:
                break
            pick.append(i)
        if len(pick) == n:
            attempts.append(sorted(pick))

    # 基础:同类牌从小到大取,不够用杂牌补
    base = same[:n]
    base += rest[:n - len(base)]
    if len(base) == n:
        attempts.append(sorted(base))

    # 兜底:枚举同类牌组合 + 补最小杂牌
    k = min(n, len(same))
    for combo in itertools.combinations(same, k):
        cand = list(combo) + rest[:n - k]
        if len(cand) == n:
            cand = sorted(cand)
            if cand not in attempts:
                attempts.append(cand)
        if len(attempts) >= limit:
            break

    # 最终兜底:全手牌任意组合(小范围)
    for combo in itertools.combinations(range(len(hand)), n):
        cand = sorted(combo)
        if cand not in attempts:
            attempts.append(cand)
        if len(attempts) >= limit:
            break
    return attempts


def script_play(player, game_id):
    """用脚本逻辑替 player 出牌:领出打最小单张,跟牌按规则尝试组合。"""
    tag = f"座位{player.seat}"
    g = player.table(game_id)
    hand = g.get("myHand") or []
    trick = g.get("currentTrick") or []
    trump_suit, trump_rank = g.get("trumpSuit"), g.get("trumpRank")

    if not trick:  # 领出:打最小的非主单张
        key = lambda i: (is_trump(hand[i], trump_suit, trump_rank),
                         VALUE_ORDER.get(hand[i].get("value"), 0))
        idx = min(range(len(hand)), key=key)
        status, data = api("POST", f"/api/game/{game_id}/play", player.token,
                           {"cardIndices": [idx]})
        must(status, data, f"{tag} 领出")
        print(f"    {tag} 领出 {card_str(hand[idx])}")
        return

    lead_cards = trick[0].get("cards") or []
    for cand in follow_attempts(hand, lead_cards, trump_suit, trump_rank):
        status, data = api("POST", f"/api/game/{game_id}/play", player.token,
                           {"cardIndices": cand})
        if status < 400 and data.get("success"):
            played = " ".join(card_str(hand[i]) for i in cand)
            print(f"    {tag} 跟牌 {played}")
            return
    print(f"[FATAL] {tag} 跟牌失败,手牌: {[card_str(c) for c in hand]}, "
          f"领出: {[card_str(c) for c in lead_cards]}")
    sys.exit(1)


def main():
    global BASE
    ap = argparse.ArgumentParser()
    ap.add_argument("--base", default=BASE)
    ap.add_argument("--watch", action="store_true",
                    help="观战模式:固定账号、开局前暂停等你进浏览器、放慢出牌节奏")
    ap.add_argument("--delay", type=float, default=None,
                    help="每手牌之间的延时秒数(观战模式默认 1.5,否则 0)")
    args = ap.parse_args()
    BASE = args.base
    delay = args.delay if args.delay is not None else (1.5 if args.watch else 0)

    # 1. 账号(观战模式用固定账号 bot_p1~bot_p5,密码 pass1234)
    players = register_players(5, fixed=args.watch)
    p1 = players[0]

    # 2. 建房 + 加入
    status, data = api("POST", "/api/game/create", p1.token,
                       {"name": "模拟对局房"})
    must(status, data, "建房")
    game_id = data["gameId"]
    print(f"[OK] 房间创建成功 gameId={game_id}")
    for p in players[1:]:
        status, data = api("POST", f"/api/game/{game_id}/join", p.token)
        must(status, data, f"{p.name} 加入房间")
    print("[OK] 5 人已全部进入房间")

    if args.watch:
        print("\n" + "=" * 55)
        print("  观战模式:请在浏览器完成以下操作")
        print(f"  1. 打开 http://localhost:5175/login")
        print(f"  2. 登录账号 bot_p1  密码 pass1234")
        print(f"  3. 打开 http://localhost:5175/game/table/{game_id}")
        print("=" * 55)
        countdown = 15
        for t in range(countdown, 0, -1):
            print(f"\r  {t}s 后自动开始游戏...", end="", flush=True)
            time.sleep(1)
        print()

    # 3. 全员 ready(最后一人触发自动开局)
    for p in players:
        status, data = api("POST", f"/api/game/{game_id}/ready", p.token)
        must(status, data, f"{p.name} ready")
    print("[OK] 全员已准备,游戏开始")

    # 记录各自座位
    for p in players:
        p.seat = p.table(game_id).get("myPosition")
    seat_map = {p.seat: p for p in players}
    print("[OK] 座位分配: " + ", ".join(f"{p.name}=座位{p.seat}" for p in players))

    # 4. 发牌
    dealt = 0
    while True:
        status, data = api("POST", f"/api/game/{game_id}/deal-next", p1.token)
        must(status, data, "发牌")
        dealt += 1
        if data.get("complete"):
            break
    print(f"[OK] 发牌完成,共调用 deal-next {dealt} 次")

    # 5. 叫庄
    g = p1.table(game_id)
    trump_rank = g.get("trumpRank") or "2"
    caller, idxs = find_caller(players, game_id, trump_rank)
    if caller:
        status, data = api("POST", f"/api/game/{game_id}/call-dealer",
                           caller.token, {"cardIndices": idxs})
        must(status, data, f"{caller.name} 亮庄")
        print(f"[OK] {caller.name}(座位{caller.seat}) 亮庄 {len(idxs)} 张级牌")
    for p in players:
        if p is caller:
            continue
        api("POST", f"/api/game/{game_id}/pass-call", p.token)  # 失败可忽略

    # 轮询等待定庄(倒计时由 GET table 惰性驱动)
    print("[..] 等待叫庄倒计时...")
    while True:
        g = p1.table(game_id)
        if g.get("status") not in ("calling", "dealing"):
            break
        api("GET", f"/api/game/{game_id}/check-countdown", p1.token)
        time.sleep(1)
    dealer_seat = g.get("dealerSeat")
    dealer = seat_map[dealer_seat]
    print(f"[OK] 定庄:座位{dealer_seat}({dealer.name}),状态={g.get('status')}")

    # 6. 扣底(等待底牌并入庄家手牌 → 38 张)
    while True:
        g = dealer.table(game_id)
        if g.get("status") != "discarding":
            break
        hand = g.get("myHand") or []
        if len(hand) >= 38:
            idxs = pick_discard(hand, g.get("trumpSuit"), g.get("trumpRank"))
            status, data = api("POST", f"/api/game/{game_id}/discard-bottom",
                               dealer.token, {"cardIndices": idxs})
            must(status, data, "庄家扣底")
            print(f"[OK] 庄家扣底 7 张: "
                  + " ".join(card_str(hand[i]) for i in idxs))
            break
        time.sleep(1)

    # 7. 叫朋友(叫一张非主花色的 A)
    while True:
        g = dealer.table(game_id)
        if g.get("status") != "calling_friend":
            if g.get("status") == "playing":
                break
            time.sleep(1)
            continue
        trump_suit = g.get("trumpSuit")
        suits = ["spades", "hearts", "clubs", "diamonds"]
        suit = next((s for s in suits if s != trump_suit), "spades")
        status, data = api("POST", f"/api/game/{game_id}/call-friend",
                           dealer.token,
                           {"suit": suit, "value": "A", "position": 1})
        must(status, data, "叫朋友")
        print(f"[OK] 庄家叫朋友: {suit} A 第1张")
        break

    # 8. 出牌循环
    print("[..] 进入出牌阶段" + (f"(每手延时 {delay}s)" if delay else ""))
    tricks = 0
    while True:
        g = p1.table(game_id)
        st = g.get("status")
        if st == "finished":
            break
        if st != "playing":
            time.sleep(1)
            continue
        cur = g.get("currentPlayer")
        if cur == p1.seat or args.watch:
            # 观战模式:所有座位都由脚本逐手出牌,便于在浏览器观看
            script_play(seat_map[cur], game_id)
        else:
            # ai-play 会替座位 2-5 连续出牌,直到轮回座位1 或结束;
            # 若后端 AI 选不出合法牌(400),重新获取当前座位由脚本代打
            status, data = api("POST", f"/api/game/{game_id}/ai-play", p1.token)
            if status >= 400:
                g2 = p1.table(game_id)
                if g2.get("status") != "playing":
                    continue
                cur2 = g2.get("currentPlayer")
                print(f"    [warn] ai-play 失败({data.get('error')}),"
                      f"脚本替座位{cur2} 出牌")
                script_play(seat_map[cur2], game_id)
        tricks += 1
        remain = len(g.get("myHand") or [])
        if tricks % 10 == 0:
            print(f"    ...进行中,座位1 剩余手牌 {remain} 张")
        time.sleep(delay if delay else 0.2)

    # 9. 结果
    g = p1.table(game_id)
    print("\n========== 对局结束 ==========")
    print(f"闲家得分 totalPoints: {g.get('totalPoints')}")
    rr = g.get("roundResults")
    if rr:
        print("本局结果 roundResults:")
        print(json.dumps(rr, ensure_ascii=False, indent=2))
    print(f"各座位得分 scores: {g.get('scores')}")
    print("玩家等级: " + ", ".join(
        f"座位{pl.get('position')} {pl.get('username')} -> {pl.get('level')}"
        for pl in (g.get("players") or [])))


if __name__ == "__main__":
    main()
