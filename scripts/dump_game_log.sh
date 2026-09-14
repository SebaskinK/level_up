#!/usr/bin/env bash
# 把一整局的动作日志导出成人能读的时间线 txt。
#
#   ./scripts/dump_game_log.sh              # 最近一局
#   ./scripts/dump_game_log.sh <对局ID>      # 指定一局
#   ./scripts/dump_game_log.sh <对局ID> out.txt
set -euo pipefail

PG_CONTAINER="${PG_CONTAINER:-levelup-pg}"
PG_USER="${DB_USER:-postgres}"
PG_DB="${DB_NAME:-level_up}"

psql_q() { docker exec "$PG_CONTAINER" psql -U "$PG_USER" -d "$PG_DB" -tAF'|' -c "$1"; }

GAME_ID="${1:-}"
if [[ -z "$GAME_ID" ]]; then
  GAME_ID=$(psql_q "select game_id from game_action_logs where action_type='playing_start' order by id desc limit 1")
fi
if [[ -z "$GAME_ID" ]]; then
  echo "找不到任何对局" >&2
  exit 1
fi

OUT="${2:-game_${GAME_ID}.txt}"

{
  echo "对局 $GAME_ID 完整时间线"
  echo "导出时间: $(date '+%Y-%m-%d %H:%M:%S')"
  echo

  echo "── 各阶段记录条数 ──"
  psql_q "select action_type || ' × ' || count(*)
          from game_action_logs where game_id='$GAME_ID'
          group by action_type order by min(id)"
  echo

  echo "── 时间线 ──"
  psql_q "
  select
    lpad(row_number() over (order by id)::text, 4, ' ') || '  ' ||
    to_char(timestamp, 'HH24:MI:SS') || '  ' ||
    rpad(action_type, 17, ' ') ||
    case when player_seat > 0 then '座位' || player_seat else '  -  ' end || '  ' ||
    case action_type
      when 'game_create' then '建房，' || (action_data->>'mode') || '，' ||
           jsonb_array_length(action_data->'seats') || ' 个座位'
      when 'game_start' then '开局，级牌' || (action_data->>'trumpRank') ||
           '，起始发牌人座位' || (action_data->>'startingDealerSeat') ||
           '，每人' || (action_data->>'totalCardsPerPlayer') || '张'
      when 'dealing_complete' then '发牌完成，共发' || (action_data->>'totalDealt') ||
           '张，底牌' || jsonb_array_length(action_data->'bottomCards') || '张，转入' || (result_data->>'callPhase')
      when 'pass_call' then '不叫庄'
      when 'flip_bottom' then '翻开第' || (action_data->>'flipped_count') || '张底牌：' ||
           (case action_data->'card'->>'suit' when 'spades' then '黑' when 'hearts' then '红'
                                              when 'clubs' then '梅' when 'diamonds' then '方'
                                              else '王' end) || (action_data->'card'->>'value')
      when 'call_dealer' then '亮庄：' || coalesce(action_data->>'suit','') || ' ' ||
           coalesce(action_data->>'count','') || '张'
      when 'dealer_settled' then '定庄，庄家座位' || (action_data->>'dealerSeat') ||
           '，主花色' || coalesce(nullif(action_data->>'trumpSuit',''),'(无)') ||
           case when (result_data->>'byFlip')::bool then '（翻牌定庄）' else '（亮庄）' end
      when 'discard_bottom' then '扣底' || jsonb_array_length(action_data->'discarded_cards') || '张'
      when 'call_friend' then '叫朋友：' || (action_data->>'called_card') ||
           ' 第' || (action_data->>'position') || '张'
      when 'playing_start' then '开打，主花色' || (action_data->>'trumpSuit') ||
           '，级牌' || (action_data->>'trumpRank') ||
           '，庄家座位' || (action_data->>'dealerSeat') ||
           '，先出座位' || (action_data->>'firstPlayer')
      when 'play_cards' then '出 ' || coalesce(action_data->>'play_type','?') || '：' ||
           (select string_agg(
              case c->>'suit' when 'spades' then '黑' when 'hearts' then '红'
                              when 'clubs' then '梅' when 'diamonds' then '方'
                              else '王' end || (c->>'value'), ' ')
            from jsonb_array_elements(action_data->'cards') c) ||
           case when (action_data->>'is_lead')::bool then '  (领出)' else '' end
      when 'trick_complete' then '第' || (action_data->>'trick_number') || '墩结束，座位' ||
           (result_data->>'winner_seat') || '赢，得' || (result_data->>'points_collected') || '分'
      when 'friend_revealed' then '朋友亮相：座位' || (result_data->>'friendSeat') ||
           ' 打出了 ' || (action_data->>'calledCard') || '（第' || (action_data->>'position') || '张）'
      when 'throw_accepted' then '甩牌成功，' || (result_data->>'count') || '张'
      when 'throw_rejected' then '甩牌失败：' || (result_data->>'reason') ||
           '，收回后只出' || (result_data->>'keptCount') || '张'
      when 'pass_turn' then '这一手不出'
      when 'ai_auto_discard' then '机器人庄家自动扣底（选最小7张）'
      when 'ai_call_friend_failed' then '机器人叫朋友失败：' || (result_data->>'error')
      when 'bottom_kick' then case when (action_data->>'kicked')::bool
             then '抠底成功，倍数×' || (result_data->>'multiplier') ||
                  '，底牌得' || (result_data->>'bottomPoints') || '分'
             else '未抠底（庄家方赢了最后一墩）' end
      when 'game_end' then '本局结束'
      else '' end
  from game_action_logs
  where game_id='$GAME_ID'
  order by id"

  echo
  echo "── 开局手牌（playing_start 快照）──"
  psql_q "
  select '座位' || k || '  ' ||
         rpad(action_data->'hands'->k->>'userId', 22, ' ') ||
         case when (action_data->'hands'->k->>'isAI')::bool then '机器人' else '真人  ' end ||
         '  ' || jsonb_array_length(action_data->'hands'->k->'cards') || '张'
  from game_action_logs, jsonb_object_keys(action_data->'hands') k
  where game_id='$GAME_ID' and action_type='playing_start' order by k"

  echo
  echo "── 本局结算 ──"
  psql_q "
  select rpad(r->>'user_id', 22, ' ') || '  ' ||
         (r->>'old_level') || ' → ' || (r->>'new_level') ||
         case when (r->>'is_winner')::bool then '   胜' else '   负' end ||
         '   抓分方得' || (r->>'score') || '分'
  from game_action_logs, jsonb_array_elements(action_data->'results') r
  where game_id='$GAME_ID' and action_type='game_end'"
} > "$OUT"

echo "已导出: $OUT ($(wc -l < "$OUT") 行)"
