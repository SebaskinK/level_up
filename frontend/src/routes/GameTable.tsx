import { useParams, Navigate, useNavigate, useLocation, Link } from 'react-router-dom';
import { cn } from '@/lib/utils';
import { Loader2, ArrowLeft, Play, Film, User, Menu, X, LogOut } from 'lucide-react';
import { isJoker, getJokerImageUrl } from '@/lib/card';
import PlayingCard from '@/components/game/PlayingCard';
import { Button } from '@/components/ui/button';
import PlayerHand from '@/components/game/PlayerHand';
import FinishedPanel from '@/components/game/FinishedPanel';
import CallDealerDialog from '@/components/game/CallDealerDialog';
import DiscardDialog from '@/components/game/DiscardDialog';
import CallFriendDialog from '@/components/game/CallFriendDialog';
import {
    useGameTable,
    usePlayCards,
    useAiPlay,
    useStartSinglePlayerGame,
    useCallDealer,
    usePassCall,
    useDiscardBottomCards,
    useCallFriend,
    useJoinGame,
    usePlayerReady,
    useCancelReady,
    useReadyStatus,
    useDealNextCard,
    useNextRound,
} from '@/hooks/useGame';
import { useGameStore } from '@/store/gameStore';
import { useAuthStore } from '@/store/authStore';
import { useLogout } from '@/hooks/useAuth';
import { EGameStatus, ECardSuit, ICard } from '@/types';
import { useEffect, useRef, useState} from 'react';
import { createPortal } from 'react-dom';

// 花色符号映射
const SUIT_SYMBOLS: Record<string, string> = {
    hearts: '♥',
    diamonds: '♦',
    clubs: '♣',
    spades: '♠',
    joker: 'Joker',
};

// 获取花色显示类名
const getSuitClass = (suit: string): string => {
    if (suit === 'hearts' || suit === 'diamonds') return 'text-red-500';
    if (suit === 'clubs' || suit === 'spades') return 'text-slate-200';
    if (suit === 'joker') return 'text-purple-400';
    return 'text-slate-200';
};

export default function GameTable() {
    const { id } = useParams<{ id: string }>();
    const gameId = id ?? '';
    const navigate = useNavigate();
    const location = useLocation();
    const { isAuthenticated, user } = useAuthStore();
    const { selectedCardIndices, clearSelection } = useGameStore();
    const isSinglePlayerRoute = location.pathname.startsWith('/game/singleplayer/');


    const { data, isLoading, isError } = useGameTable(gameId);
    const playCards = usePlayCards(gameId);
    const aiPlay = useAiPlay(gameId);
    const callDealerMutation = useCallDealer(gameId);
    const passCallMutation = usePassCall(gameId);
    const discardMutation = useDiscardBottomCards(gameId);
    const callFriendMutation = useCallFriend(gameId);
    const startSinglePlayerMutation = useStartSinglePlayerGame(gameId);
    const joinGameMutation = useJoinGame();
    const readyMutation = usePlayerReady(gameId);
    const cancelReadyMutation = useCancelReady(gameId);
    const { data: readyStatusData } = useReadyStatus(gameId);
    const dealNextCardMutation = useDealNextCard(gameId);
    const nextRoundMutation = useNextRound(gameId);
    const dealingIntervalRef = useRef<ReturnType<typeof setInterval> | null>(null);
    const hasAttemptedJoinRef = useRef(false);
    const lastAITurnRef = useRef<number | null>(null);
const game = data?.game;

    // 安全属性，防止 undefined 报错
    const dealerTeam = game?.dealerTeam ?? [];
    const players = game?.players ?? [];

    // DEBUG: Log game data
    useEffect(() => {
        if (game) {
            console.log('=== GAME DATA DEBUG ===');
            console.log('Total players:', players.length);
            console.log('Players:', players);
            console.log('My position:', game.myPosition);
            console.log('=======================');
        }
    }, [game]);

    // 翻底牌定庄完成后无需独立公告：级牌与底牌已合并到"扣底牌"对话框内统一展示。
    //
    // 定庄成功展示时长（毫秒）：从 dealerConfirmedAt 起，在此时间内保留"✓ 定庄成功"提示，
    // 同时让 7 张底牌完整呈现给所有玩家观察。
    // 仅适用于"翻底定庄"路径；亮庄/反庄定庄不展示底牌（详见 rules/03-bidding.md §3.4 互斥说明）。
    const DEALER_CONFIRM_SHOW_MS = 5000;
    // 翻牌→定庄成功→扣牌之间的淡出过渡时长（毫秒）。
    const DEALER_CONFIRM_FADE_MS = 300;

    // 翻牌动画保留期：每当 flippedBottomCards 数量发生变化（即新翻开一张），
    // 把"翻牌画面允许显示到何时"延后到 now + (1.5s 翻牌 + 0.5s 停顿) = 2s。
    // 即使后端因定庄已切到 finished / discarding，前端也会继续渲染翻牌画面直到该时间点，
    // 确保任何一张触发定庄的牌都能完整播放翻牌动画。
    const [flipHoldUntil, setFlipHoldUntil] = useState<number>(0);
    const [nowTick, setNowTick] = useState<number>(() => Date.now());
    const lastFlippedCountRef = useRef<number>(0);
    // 本地翻牌计数：用于确保每张牌都有一个独立的渲染帧从"卡背(rotateY 0)"过渡到"卡面(rotateY 180deg)"
    // 工作流程：
    //   1) 后端 flippedBottomCards.length 增长 → 把待翻数量加入队列
    //   2) 队列按 1.6s 间隔逐张推进 localFlippedCount，
    //      每次先 setState 触发一个仅修改"卡背"为"待翻"占位的渲染（保持 rotateY 0），
    //      下一帧再设为已翻（rotateY 180deg），让浏览器捕获 transition 起点。
    const [localFlippedCount, setLocalFlippedCount] = useState<number>(0);
    // 定庄确认时间戳：翻牌动画播完后，后端已定庄（callPhase=finished / status=discarding）时记录，
    // 翻牌画面会再保留 1 秒展示"定庄成功"提示，之后才让扣牌对话框弹出。
    const [dealerConfirmedAt, setDealerConfirmedAt] = useState<number>(0);

    const [showDiscardDialog, setShowDiscardDialog] = useState(false);
    const [showCallFriendDialog, setShowCallFriendDialog] = useState(false);
    const [showLastTrick, setShowLastTrick] = useState(true); // 是否显示上一轮出牌(默认显示)
    const [topMenuOpen, setTopMenuOpen] = useState(false); // 顶部三横杠菜单是否展开

    // 本地平滑倒计时：服务器只每隔几秒推送，前端本地按秒递减，避免显示卡顿
    const [localCountdown, setLocalCountdown] = useState<number | null>(null);
    useEffect(() => {
        if (game?.callPhase === 'counting' && typeof game.callCountdown === 'number') {
            setLocalCountdown(game.callCountdown);
        } else {
            setLocalCountdown(null);
        }
    }, [game?.callPhase, game?.callCountdown]);
    useEffect(() => {
        if (localCountdown == null || localCountdown <= 0) return;
        const t = setTimeout(() => {
            setLocalCountdown((c) => (c == null ? null : Math.max(0, c - 1)));
        }, 1000);
        return () => clearTimeout(t);
    }, [localCountdown]);

    // 监听翻牌数量变化：每当后端 flippedBottomCards.length 增长，
    // 1) 把保留截止时间设置为 now + 2s（确保最后一张动画完整播完，详见 flipHoldUntil 注释）
    // 2) 按 1.6s 间隔逐张推进 localFlippedCount，使每张牌都能播放完整的 1.5s 翻转动画。
    //    即便后端 polling 一次性把 length 从 N 跳到 N+k，前端也会逐张播放。
    useEffect(() => {
        const count = game?.flippedBottomCards?.length ?? 0;
        if (count > lastFlippedCountRef.current) {
            lastFlippedCountRef.current = count;
            setFlipHoldUntil(Date.now() + 2000);
        }
        // 当 status 退出 CALLING（例如重开一局回到 WAITING/DEALING），重置计数与定庄确认时间戳
        if (game?.status !== EGameStatus.CALLING
            && game?.status !== EGameStatus.DISCARDING
            && game?.status !== EGameStatus.CALLING_FRIEND) {
            lastFlippedCountRef.current = count;
            setLocalFlippedCount(count);
            setDealerConfirmedAt(0);
        }
    }, [game?.flippedBottomCards?.length, game?.status]);

    // 逐张推进 localFlippedCount，直至追上后端的 flippedBottomCards.length
    // 每次推进先确保上一张的翻转动画已基本完成（1.5s + 0.1s 缓冲）
    useEffect(() => {
        const targetCount = game?.flippedBottomCards?.length ?? 0;
        if (localFlippedCount >= targetCount) return;
        const delay = localFlippedCount === 0 ? 50 : 1600;
        const t = setTimeout(() => {
            setLocalFlippedCount(c => Math.min(targetCount, c + 1));
            setFlipHoldUntil(Date.now() + 2000);
        }, delay);
        return () => clearTimeout(t);
    }, [localFlippedCount, game?.flippedBottomCards?.length]);

    // 检测定庄确认：必须同时满足：
    //   1) 后端 callPhase === 'finished'（后端确认不会再翻牌）
    //   2) localFlippedCount 已追上后端的 flippedBottomCards.length（前端最后一张动画已开始/结束）
    //   3) 至少经过该张牌的 0.5s 停顿（即翻牌完成后再停 0.5s 才判定为"定庄完成"）
    // 这样可以避免出现"先弹出定庄成功 → 又退回翻牌中"的错乱
    useEffect(() => {
        if (dealerConfirmedAt > 0) return;
        const targetCount = game?.flippedBottomCards?.length ?? 0;
        if (targetCount === 0) return;
        if (localFlippedCount < targetCount) return;
        // 必须 callPhase 已经 finished，避免后端还可能继续翻牌时提前误判
        if (game?.callPhase !== 'finished'
            && game?.status !== EGameStatus.DISCARDING
            && game?.status !== EGameStatus.CALLING_FRIEND) return;
        // 还要确保最后一张牌的"1.5s 翻 + 0.5s 停顿"已基本播完
        // flipHoldUntil 在每次 localFlippedCount 推进时被设置为 now + 2000ms
        if (Date.now() < flipHoldUntil) return;
        setDealerConfirmedAt(Date.now());
    }, [localFlippedCount, game?.flippedBottomCards?.length, game?.callPhase, game?.status, dealerConfirmedAt, flipHoldUntil, nowTick]);

    // 保留期内每 100ms 推进 nowTick，使条件判断能及时重渲染
    useEffect(() => {
        const shouldTick = nowTick < flipHoldUntil
            || (dealerConfirmedAt > 0 && nowTick < dealerConfirmedAt + DEALER_CONFIRM_SHOW_MS + DEALER_CONFIRM_FADE_MS + 100)
            || (localFlippedCount > 0 && localFlippedCount < (game?.flippedBottomCards?.length ?? 0));
        if (!shouldTick) return;
        const t = setTimeout(() => setNowTick(Date.now()), 100);
        return () => clearTimeout(t);
    }, [nowTick, flipHoldUntil, dealerConfirmedAt, localFlippedCount, game?.flippedBottomCards?.length]);

    const logoutMutation = useLogout();
    const seatRefs = useRef<Record<number, HTMLDivElement | null>>({}); // 玩家头像 DOM 引用（座位 -> 元素）
    const tableSurfaceRef = useRef<HTMLDivElement | null>(null); // 桌布 DOM 引用
    const cardMatRef = useRef<HTMLDivElement | null>(null); // 牌垫区域（飞牌 overlay 的父级）DOM 引用
    const [cardMatEl, setCardMatEl] = useState<HTMLDivElement | null>(null); // 用于触发 portal 在 ref 挂载后重渲染
    const [seatTargets, setSeatTargets] = useState<Record<number, { x: number; y: number }>>({});

    // 计算当前轮中谁最大（平牌时先出的大）
    const getCurrentWinner = () => {
        if (!game?.currentTrick || game.currentTrick.length === 0) return null;
        if (!game.trumpSuit || !game.trumpRank) return null;

        let winner = game.currentTrick[0];

        for (const played of game.currentTrick) {
            const cmp = compareCards(played.cards[0], winner.cards[0], game.trumpSuit, game.trumpRank);
            if (cmp > 0 || (cmp === 0 && game.currentTrick.indexOf(played) < game.currentTrick.indexOf(winner))) {
                winner = played;
            }
        }
        return winner;
    };

    // 计算上一轮中谁最大（平牌时先出的大）
    // const getLastTrickWinner = () => { ... } // 暂未使用，保留 showLastTrick 直接展示牌

    // 比较两张牌的大小，返回 1: a大, -1: b大, 0: 相等
    const compareCards = (a: ICard, b: ICard, trumpSuit: string, trumpRank: string): number => {
        const aIsTrump = isCardTrump(a, trumpSuit, trumpRank);
        const bIsTrump = isCardTrump(b, trumpSuit, trumpRank);

        // 主牌比副牌大
        if (aIsTrump && !bIsTrump) return 1;
        if (!aIsTrump && bIsTrump) return -1;

        // 都是主牌或都是副牌
        const aStrength = getCardStrength(a, trumpSuit, trumpRank);
        const bStrength = getCardStrength(b, trumpSuit, trumpRank);

        return aStrength - bStrength;
    };

    // 判断是否是主牌
    const isCardTrump = (card: ICard, trumpSuit: string, trumpRank: string): boolean => {
        if (card.suit === 'joker') return true;
        if (card.value === trumpRank) return true;
        if (card.suit === trumpSuit) return true;
        return false;
    };

    // 获取牌的强度值
    const getCardStrength = (card: ICard, trumpSuit: string, trumpRank: string): number => {
        const baseValues: Record<string, number> = { '2': 2, '3': 3, '4': 4, '5': 5, '6': 6, '7': 7, '8': 8, '9': 9, '10': 10, 'J': 11, 'Q': 12, 'K': 13, 'A': 14, 'Small': 15, 'Big': 16 };

        if (card.suit === 'joker') {
            return card.value === 'Big' ? 100 : 99;
        }

        const isTrumpRank = card.value === trumpRank;
        const isTrumpSuit = card.suit === trumpSuit;

        let strength = baseValues[card.value] || 0;

        // 主牌花色加50，同花色级牌加75（最高优先级）
        if (isTrumpSuit) strength += 50;
        if (isTrumpRank) strength += 25;

        return strength;
    };

    // 单人模式不再自动开局；改为在 WAITING 阶段显示"开始游戏"按钮，由玩家手动触发。
    // 这里只保留路由信息和 mutation 供按钮使用。

    // 跟踪上一轮完成出牌
    const prevTrickCountRef = useRef(0);
    useEffect(() => {
        if (!game) return;
        const currentCount = game.currentTrick.length;
        // 从有牌变成没牌，说明一轮结束，将最后一轮保存
        if (prevTrickCountRef.current > 0 && currentCount === 0) {
            // 当前轮已经被清空，使用 lastCompletedTrick 数据
        }
        prevTrickCountRef.current = currentCount;
    }, [game?.currentTrick?.length]);

    // 自动显示扣牌或叫朋友对话框
    useEffect(() => {
        if (!game) return;
        // 翻牌动画保留期内不抢先弹出扣底牌对话框：
        // 1) 等翻牌动画播完（localFlippedCount 追上后端数量）
        // 2) 等 flipHoldUntil 缓冲期过去
        // 3) 若已定庄，等 dealerConfirmedAt + DEALER_CONFIRM_SHOW_MS 展示"定庄成功"完毕
        const targetFlipped = game.flippedBottomCards?.length ?? 0;
        const flipAnimPending = localFlippedCount < targetFlipped;
        const inFlipHold = Date.now() < flipHoldUntil || flipAnimPending;
        const inDealerConfirmShow = dealerConfirmedAt > 0 && Date.now() < dealerConfirmedAt + DEALER_CONFIRM_SHOW_MS;
        if (game.status === EGameStatus.DISCARDING && !inFlipHold && !inDealerConfirmShow) {
            // 只有庄家才能扣牌
            if (game.dealerSeat === game.myPosition) {
                setShowDiscardDialog(true);
            } else {
                setShowDiscardDialog(false);
            }
        } else if (game.status === EGameStatus.CALLING_FRIEND) {
            // 只有庄家才会自动弹出叫朋友对话框
            if (game.dealerSeat === game.myPosition) {
                setShowCallFriendDialog(true);
            } else {
                setShowCallFriendDialog(false);
            }
        } else {
            setShowDiscardDialog(false);
            setShowCallFriendDialog(false);
        }
    }, [game?.status, game?.dealerSeat, game?.myPosition, flipHoldUntil, nowTick, localFlippedCount, game?.flippedBottomCards?.length, dealerConfirmedAt]);

    // 自动加入游戏（如果尚未加入）
    useEffect(() => {
        if (!game) return;
        if (isSinglePlayerRoute) return; // 单人模式不需要加入
        if (hasAttemptedJoinRef.current) return;
        if (!user?.id) return;

        // 检查当前用户是否已经在游戏中
        const isInGame = players?.some(p => p.id === Number(user.id));
        if (!isInGame && game.status === EGameStatus.WAITING) {
            console.log('自动加入游戏');
            hasAttemptedJoinRef.current = true;
            joinGameMutation.mutate(gameId);
        }
    }, [game?.players, game?.status, gameId, user?.id, isSinglePlayerRoute]);

    // AI自动出牌
    useEffect(() => {
        if (!game) return;
        if (game.status !== EGameStatus.PLAYING) return;

        // 查找当前玩家
        const currentPlayer = players.find(p => p.position === game.currentPlayer);
        if (!currentPlayer || !currentPlayer.isAI) {
            // 重置ref如果不是AI的回合
            lastAITurnRef.current = null;
            return;
        }

        // 检查是否已经为这个玩家的这个回合触发过AI出牌
        const turnKey = game.currentPlayer;
        if (lastAITurnRef.current === turnKey) {
            return; // 已经触发过，不再重复触发
        }

        // 如果是AI且没有正在执行操作，自动触发AI出牌
        if (!aiPlay.isPending) {
            lastAITurnRef.current = turnKey;
            setTimeout(() => aiPlay.mutate(), 500); // 延迟500ms让用户看到轮到AI了
        }
    }, [game?.currentPlayer, game?.status]);

    // 注意：自动开始游戏的逻辑已移至后端，当5人全部准备后自动触发

    // 发牌轮询逻辑：每 1 秒请求一次后端发"下一张"牌（按规则逆时针发）
    // 单人 / 多人模式下后端都会进入 DEALING 阶段，前端按 1 秒 / 张轮询，
    // 把每一张飞向对应座位
    useEffect(() => {
        if (!game) return;
        if (game.status !== EGameStatus.DEALING) {
            if (dealingIntervalRef.current) {
                clearInterval(dealingIntervalRef.current);
                dealingIntervalRef.current = null;
            }
            return;
        }

        if (!dealingIntervalRef.current && !dealNextCardMutation.isPending) {
            dealingIntervalRef.current = setInterval(() => {
                if (!dealNextCardMutation.isPending) {
                    dealNextCardMutation.mutate();
                }
            }, 200);
        }

        return () => {
            if (dealingIntervalRef.current) {
                clearInterval(dealingIntervalRef.current);
                dealingIntervalRef.current = null;
            }
        };
    }, [game?.status, dealNextCardMutation.isPending]);

    // 发牌动画：每发出一张牌（dealtCardCount 增加）就触发一次飞牌动画
    const [dealAnimKey, setDealAnimKey] = useState(0);
    const lastDealtCountRef = useRef<number>(0);
    useEffect(() => {
        if (!game) return;
        if (game.status !== EGameStatus.DEALING) {
            lastDealtCountRef.current = 0;
            return;
        }
        const count = game.dealtCardCount ?? 0;
        if (count !== lastDealtCountRef.current) {
            lastDealtCountRef.current = count;
            setDealAnimKey((k) => k + 1);
        }
    }, [game?.dealtCardCount, game?.status]);

    // 实时测量每个座位头像中心相对于"牌垫区域"中心的像素偏移，
    // 用作发牌动画飞向的目标。布局变化（窗口缩放、玩家入座、字号变化等）时自动重算。
    useEffect(() => {
        const mat = cardMatRef.current;
        if (!mat) return;
        const recompute = () => {
            const matRect = mat.getBoundingClientRect();
            if (matRect.width === 0 || matRect.height === 0) return;
            const matCx = matRect.left + matRect.width / 2;
            const matCy = matRect.top + matRect.height / 2;
            const next: Record<number, { x: number; y: number }> = {};
            for (const seat of [1, 2, 3, 4, 5]) {
                const el = seatRefs.current[seat];
                if (!el) continue;
                const r = el.getBoundingClientRect();
                next[seat] = {
                    x: r.left + r.width / 2 - matCx,
                    y: r.top + r.height / 2 - matCy,
                };
            }
            setSeatTargets(prev => {
                // 浅比较，避免无意义的 setState
                const keys = Object.keys(next);
                if (keys.length === Object.keys(prev).length &&
                    keys.every(k => prev[+k] && Math.abs(prev[+k].x - next[+k].x) < 0.5 && Math.abs(prev[+k].y - next[+k].y) < 0.5)) {
                    return prev;
                }
                return next;
            });
        };
        recompute();
        const ro = new ResizeObserver(recompute);
        ro.observe(mat);
        const surface = tableSurfaceRef.current;
        if (surface) ro.observe(surface);
        for (const seat of [1, 2, 3, 4, 5]) {
            const el = seatRefs.current[seat];
            if (el) ro.observe(el);
        }
        window.addEventListener('resize', recompute);
        window.addEventListener('scroll', recompute, true);
        return () => {
            ro.disconnect();
            window.removeEventListener('resize', recompute);
            window.removeEventListener('scroll', recompute, true);
        };
    }, [players.length, game?.status]);

    const showDealAnim = game?.status === EGameStatus.DEALING;
    // 把当前一张牌飞向后端告知的座位（lastDealtSeat），fallback 到座位 1
    const flyingSeat = game?.lastDealtSeat && game.lastDealtSeat >= 1 && game.lastDealtSeat <= 5
        ? game.lastDealtSeat
        : 1;
    const totalCardsPerPlayer = game?.totalCardsPerPlayer ?? 31;
    const dealtCount = game?.dealtCardCount ?? 0;
    // 第几轮（1..31）= ceil(dealtCount / 5)
    const dealtRound = dealtCount === 0 ? 1 : Math.min(totalCardsPerPlayer, Math.ceil(dealtCount / 5));
    // 飞牌目标偏移：优先用实时测量的像素偏移；测量未就绪时退回到静态比例兜底
    const FALLBACK_SEAT_OFFSETS: Record<number, { x: string; y: string }> = {
        1: { x: '-40%', y: '-40%' },
        2: { x: '-40%', y: '-20%' },
        3: { x: '-40%', y: '0%'   },
        4: { x: '-40%', y: '20%'  },
        5: { x: '-40%', y: '40%'  },
    };
    const measured = seatTargets[flyingSeat];
    const flyOffset = measured
        ? { x: `${Math.round(measured.x)}px`, y: `${Math.round(measured.y)}px` }
        : (FALLBACK_SEAT_OFFSETS[flyingSeat] ?? FALLBACK_SEAT_OFFSETS[1]);

    if (!isAuthenticated) return <Navigate to="/login" replace />;
    if (!gameId) return <Navigate to="/game" replace />;

    if (isLoading) {
        return (
            <div className="flex min-h-[calc(100vh-4rem)] items-center justify-center">
                <Loader2 className="h-8 w-8 animate-spin text-amber-500" />
            </div>
        );
    }

    if (isError || !data?.success || !game) {
        return (
            <div className="flex min-h-[calc(100vh-4rem)] flex-col items-center justify-center gap-4">
                <p className="text-muted-foreground">无法加载游戏数据</p>
                <Button variant="outline" onClick={() => navigate('/game')}>
                    返回大厅
                </Button>
            </div>
        );
    }

    const handlePlay = () => {
        if (selectedCardIndices.size === 0) return;
        // 不再限制出牌数量，用户可以选择任意数量的牌
        playCards.mutate(
            { cardIndices: Array.from(selectedCardIndices) },
            { onSuccess: () => clearSelection() },
        );
    };

    const handleCallDealer = (cardIndices: number[]) => {
        callDealerMutation.mutate(
            { cardIndices },
            { onSuccess: () => {
                    clearSelection();
                }},
        );
    };

    const handleDiscard = (cardIndices: number[]) => {
        discardMutation.mutate(
            cardIndices,
            { onSuccess: () => {
                    clearSelection();
                    setShowDiscardDialog(false);
                }},
        );
    };

    const handleCallFriend = (suit: ECardSuit, value: string, position: number) => {
        callFriendMutation.mutate(
            { suit, value, position },
            { onSuccess: () => {
                    setShowCallFriendDialog(false);
                }},
        );
    };

    return (
        <div className="flex h-screen flex-col">
            {/* 结算面板（游戏结束时显示） */}
            {game.status === EGameStatus.FINISHED && (
                <FinishedPanel
                    game={game}
                    userId={user?.id}
                    onNextRound={() => nextRoundMutation.mutate()}
                    isNextRoundPending={nextRoundMutation.isPending}
                />
            )}
            <div className="relative flex items-center justify-between border-b border-white/10 px-2 py-1 bg-black/30 backdrop-blur-sm flex-shrink-0">
                <Button variant="ghost" size="sm" className="h-7 px-2 gap-1 text-white/80 hover:text-white hover:bg-white/10" onClick={() => navigate('/game')}>
                    <ArrowLeft className="h-4 w-4" />
                    <span className="hidden sm:inline text-xs">返回</span>
                </Button>
                <div className="flex items-center gap-1">
                    <span className={cn(
                        "status-badge-glass rounded-full px-2 py-0.5 text-[10px] sm:text-xs",
                        game.status === EGameStatus.PLAYING ? 'border-emerald-400/40 text-emerald-300' :
                        game.status === EGameStatus.WAITING ? 'border-white/30 text-white/70' :
                        'border-purple-400/40 text-purple-200'
                    )}>
                        {game.status === EGameStatus.WAITING
                            ? '等待中'
                            : game.status === EGameStatus.DEALING
                                ? '发牌中'
                                : game.status === EGameStatus.CALLING
                                    ? (game.callPhase === 'flipping'
                                        ? '翻底定庄'
                                        : `亮庄${game.callCountdown ? ` (${game.callCountdown}s)` : ''}`)
                                    : game.status === EGameStatus.CALLING_FRIEND
                                        ? '叫朋友'
                                        : game.status === EGameStatus.DISCARDING
                                            ? '扣牌'
                                            : game.status === EGameStatus.PLAYING
                                                ? '进行中'
                                                : '已结束'}
                    </span>
                    {game.currentLevel && (
                        <span className="status-badge-glass rounded-full px-1.5 py-0.5 text-[10px] sm:text-xs border-amber-400/40 text-amber-300 inline-flex gap-1">
                            <span className="opacity-60">级</span>
                            <span className="font-bold">{game.currentLevel}</span>
                        </span>
                    )}
                    <Button
                        variant="ghost"
                        size="icon"
                        className="h-7 w-7 text-white/80 hover:text-white hover:bg-white/10"
                        onClick={() => setTopMenuOpen(o => !o)}
                        aria-label="菜单"
                    >
                        {topMenuOpen ? <X className="h-4 w-4" /> : <Menu className="h-4 w-4" />}
                    </Button>
                </div>
                {topMenuOpen && (
                    <>
                        <div
                            className="fixed inset-0 z-40 bg-black/30"
                            onClick={() => setTopMenuOpen(false)}
                        />
                        <div className="absolute right-2 top-full mt-1 z-50 w-44 rounded-lg border border-white/15 bg-black/85 backdrop-blur-xl shadow-2xl py-1 animate-fade-in">
                            <Link
                                to="/"
                                onClick={() => setTopMenuOpen(false)}
                                className="block px-3 py-2 text-xs text-white/80 hover:bg-white/10"
                            >首页</Link>
                            <Link
                                to="/game"
                                onClick={() => setTopMenuOpen(false)}
                                className="block px-3 py-2 text-xs text-white/80 hover:bg-white/10"
                            >游戏大厅</Link>
                            <Link
                                to="/rules"
                                onClick={() => setTopMenuOpen(false)}
                                className="block px-3 py-2 text-xs text-white/80 hover:bg-white/10"
                            >游戏规则</Link>
                            <button
                                onClick={() => {
                                    setTopMenuOpen(false);
                                    navigate(`/game/replay/${gameId}`);
                                }}
                                className="w-full text-left px-3 py-2 text-xs text-white/80 hover:bg-white/10 flex items-center gap-2"
                            ><Film className="h-3 w-3" />回放</button>
                            <div className="my-1 h-px bg-white/10" />
                            {isAuthenticated && user ? (
                                <button
                                    onClick={() => {
                                        setTopMenuOpen(false);
                                        logoutMutation.mutate();
                                    }}
                                    className="w-full text-left px-3 py-2 text-xs text-white/80 hover:bg-white/10 flex items-center gap-2"
                                >
                                    <LogOut className="h-3 w-3" />退出 ({user.username})
                                </button>
                            ) : (
                                <>
                                    <Link
                                        to="/login"
                                        onClick={() => setTopMenuOpen(false)}
                                        className="block px-3 py-2 text-xs text-white/80 hover:bg-white/10"
                                    >登录</Link>
                                    <Link
                                        to="/register"
                                        onClick={() => setTopMenuOpen(false)}
                                        className="block px-3 py-2 text-xs text-white/80 hover:bg-white/10"
                                    >注册</Link>
                                </>
                            )}
                        </div>
                    </>
                )}
            </div>

{/* 主体区域 */}
            <div className="flex flex-1 min-h-0 overflow-hidden flex-row">
                {/* ====== 游戏桌面（全宽，玩家栏在桌布内部）====== */}
                <div className="flex-1 min-h-0 relative flex flex-col overflow-hidden">
                    {/* 游戏桌面 */}
                    <div className="flex-1 min-h-0 relative flex items-stretch justify-center p-1 sm:p-4 overflow-hidden">
                        <div
                            ref={tableSurfaceRef}
                            className="relative w-full h-full max-w-3xl rounded-xl sm:rounded-3xl border-2 sm:border-[3px] border-white/20 game-table-surface shadow-inner overflow-hidden"
                        >
                            {/* ====== 桌布内左侧玩家栏：5 行均分，顶满高度 ====== */}
                            <div className="absolute inset-y-0 left-0 w-14 sm:w-20 md:w-24 flex flex-col z-20 border-r border-white/10 bg-black/25 backdrop-blur-sm">
                                {[1, 2, 3, 4, 5].map(seat => {
                                    const player = players.find(p => p.position === seat);
                                    if (!player) {
                                        return (
                                            <div key={seat} className="flex-1 min-h-0 flex items-center justify-center border-b border-white/5 last:border-b-0">
                                                <span className="text-[9px] text-white/30">空位{seat}</span>
                                            </div>
                                        );
                                    }
                                    const isMe = player.id === Number(user?.id);
                                    const isCurrentTurn = game.currentPlayer === player.position;
                                    const isDealer = dealerTeam.includes(player.id);
                                    const isOnFire = game.friendRevealed && (isDealer || player.position === game.friendSeat);
                                    const isSoloMode = game.friendRevealed && game.friendSeat === game.dealerSeat;
                                    const biggestCard = getCurrentWinner();
                                    const isBiggest = biggestCard && biggestCard.playerId === player.id;
                                    const isThrowBlocker = game.throwBlocker === player.position;
                                    const score = game.scores?.[player.id] || 0;
                                    return (
                                        <div
                                            key={seat}
                                            ref={el => { seatRefs.current[seat] = el; }}
                                            data-seat={seat}
                                            className={cn(
                                                'flex-1 min-h-0 flex flex-col items-center justify-center px-0.5 py-0.5 border-b border-white/5 last:border-b-0 relative transition-all',
                                                isThrowBlocker ? 'bg-red-500/20' : isCurrentTurn ? 'bg-purple-500/15' : '',
                                                isOnFire && (isSoloMode ? 'fire-blue' : 'fire-border'),
                                                isBiggest && game.currentTrick.length > 0 && 'ring-1 ring-inset ring-yellow-400'
                                            )}
                                        >
                                            {isOnFire && <div className={isSoloMode ? 'fire-particles-blue' : 'fire-particles'} />}
                                            <div className={cn(
                                                'relative flex items-center justify-center rounded-full border h-7 w-7 sm:h-9 sm:w-9 flex-shrink-0',
                                                isThrowBlocker ? 'border-red-500 bg-red-500/30' : isCurrentTurn ? 'border-purple-400 bg-purple-500/25' : 'border-white/30 bg-white/10'
                                            )}>
                                                {player.isAI ? <span className="text-xs sm:text-sm z-10">🤖</span> : <User className="h-3.5 w-3.5 sm:h-5 sm:w-5 text-white/80 z-10" />}
                                                {isCurrentTurn && <div className="absolute -inset-0.5 rounded-full animate-pulse border border-purple-400/60" />}
                                            </div>
                                            <span className={cn('block truncate w-full text-center text-[9px] sm:text-[10px] mt-0.5 leading-tight', isMe ? 'text-amber-300 font-medium' : 'text-white/85')}>{player.username}</span>
                                            <div className="flex flex-wrap items-center justify-center gap-0.5 leading-none">
                                                {isDealer && <span className="text-[7px] sm:text-[8px] px-0.5 rounded-sm bg-amber-500/30 text-amber-200">庄</span>}
                                                {player.isFriend && <span className="text-[7px] sm:text-[8px] px-0.5 rounded-sm bg-pink-500/30 text-pink-200">友</span>}
                                                {isSoloMode && game.dealerSeat === player.position && <span className="text-[7px] sm:text-[8px] px-0.5 rounded-sm font-bold bg-yellow-400 text-amber-900">独</span>}
                                            </div>
                                            <span className="text-[8px] sm:text-[10px] text-white/55 leading-none mt-0.5">{player.cardCount || 0}张{score > 0 ? ` +${score}` : ''}</span>
                                        </div>
                                    );
                                })}
                            </div>

                            {/* ====== 牌垫区域：桌布除左侧玩家栏外的右半部分 ====== */}
                            <div
                                ref={(el) => { cardMatRef.current = el; setCardMatEl(el); }}
                                className="absolute inset-y-0 left-14 sm:left-20 md:left-24 right-0 flex flex-col"
                            >
                                {/* 发牌飞牌动画 overlay */}
                                {showDealAnim && (
                                    <>
                                        <div
                                            key={dealAnimKey}
                                            className="pointer-events-none absolute inset-0 z-30"
                                            aria-hidden="true"
                                        >
                                            <div
                                                className="deal-flying-card"
                                                style={
                                                    {
                                                        '--seat-x': flyOffset.x,
                                                        '--seat-y': flyOffset.y,
                                                        '--delay': '0s',
                                                    } as React.CSSProperties
                                                }
                                            >
                                                <div className="deal-card-back" />
                                            </div>
                                        </div>
                                        <div
                                            className="pointer-events-none absolute inset-0 z-30 flex items-center justify-center"
                                            data-testid="deal-anim-banner"
                                        >
                                            <div className="px-4 py-2 rounded-full text-sm sm:text-base font-bold tracking-wide text-amber-100 bg-amber-900/70 border border-amber-300/50 backdrop-blur-sm shadow-lg">
                                                发牌中 {dealtRound}/{totalCardsPerPlayer} · 座位 {flyingSeat}
                                            </div>
                                        </div>
                                    </>
                                )}

                                {/* 扣底牌阶段：下方牌垫不再展示底牌，底牌信息统一在上浮的扣底牌对话框内呈现。 */}


                                {/* 翻底牌定庄动画：
                                    - flipping 阶段：底牌从左到右逐张翻开（每张 1.5s 翻 + 0.5s 停顿）。
                                    - 翻牌动画播完后若已定庄：保留翻牌画面 1 秒展示"定庄成功"。
                                    - 之后翻牌画面消失，扣牌对话框弹出。 */}
                                {(() => {
                                    const isDealerConfirmed = dealerConfirmedAt > 0;
                                    // 展示期：[dealerConfirmedAt, dealerConfirmedAt + DEALER_CONFIRM_SHOW_MS) 内完整显示"定庄成功"
                                    // 淡出期：[dealerConfirmedAt + DEALER_CONFIRM_SHOW_MS, + DEALER_CONFIRM_SHOW_MS + DEALER_CONFIRM_FADE_MS) 渐隐
                                    // 之后翻牌区完全消失，扣牌对话框接管。
                                    const sinceConfirm = isDealerConfirmed ? Date.now() - dealerConfirmedAt : -1;
                                    const dealerConfirmShowExpired = isDealerConfirmed && sinceConfirm >= DEALER_CONFIRM_SHOW_MS;
                                    const dealerConfirmFullyGone = isDealerConfirmed && sinceConfirm >= DEALER_CONFIRM_SHOW_MS + DEALER_CONFIRM_FADE_MS;
                                    // 仅当确实走过"翻底定庄"路径时才展示底牌区：
                                    //   - 后端已经下发过至少一张翻开的底牌（game.flippedBottomCards.length > 0），或
                                    //   - 前端 localFlippedCount > 0（动画已经开始/进行）
                                    // 亮庄/反庄定庄路径下 callPhase 直接从 counting → finished，
                                    // flippedBottomCards 始终为空，此处不再展示翻底底牌区
                                    // （亮庄定庄与翻底定庄互斥，详见 rules/03-bidding.md §3.4）。
                                    const isBottomFlipPath = (game.flippedBottomCards?.length ?? 0) > 0 || localFlippedCount > 0;
                                    // 按以下条件显示翻牌画面（必须先满足 isBottomFlipPath）：
                                    //   1) 正处于 CALLING 的 flipping/finished 阶段
                                    //   2) 翻牌保留期内（最后一张动画 + 停顿尚未结束）
                                    //   3) 前端 localFlippedCount 还在追后端
                                    //   4) 处于定庄确认展示期或淡出期（dealerConfirmedAt 起 SHOW+FADE 毫秒内）
                                    const showFlipping = isBottomFlipPath
                                        && !dealerConfirmFullyGone
                                        && (
                                            (game.status === EGameStatus.CALLING
                                                && (game.callPhase === 'flipping' || game.callPhase === 'finished'))
                                            || (nowTick < flipHoldUntil && game.flippedBottomCards && game.flippedBottomCards.length > 0)
                                            || (localFlippedCount > 0 && localFlippedCount < (game.flippedBottomCards?.length ?? 0))
                                            || (isDealerConfirmed && !dealerConfirmFullyGone)
                                        )
                                        && game.bottomCards && game.bottomCards.length > 0;
                                    if (!showFlipping) return null;
                                    const dealerPlayer = game.dealerSeat != null
                                        ? players.find(p => p.position === game.dealerSeat)
                                        : undefined;
                                    // 整层透明度：进入淡出期后由 1 → 0
                                    const layerOpacity = (isDealerConfirmed && dealerConfirmShowExpired) ? 0 : 1;
                                    return (
                                    <div
                                        className="absolute inset-0 z-20 flex flex-col items-center justify-center p-2 pointer-events-none"
                                        style={{
                                            opacity: layerOpacity,
                                            transition: `opacity ${DEALER_CONFIRM_FADE_MS}ms ease-out`,
                                        }}
                                    >
                                        <div className="mb-2 flex flex-col items-center justify-center gap-1">
                                            {isDealerConfirmed ? (
                                                <>
                                                    <div className="px-3 py-1 rounded-md text-[11px] sm:text-sm text-green-100 font-bold bg-green-700/70 border border-green-300/60 shadow-lg whitespace-nowrap">
                                                        ✓ 定庄成功{dealerPlayer ? ` · 庄家：${dealerPlayer.username}` : ''}
                                                    </div>
                                                    <div className="px-2 py-0.5 rounded text-[10px] sm:text-xs text-amber-100/90 bg-black/30 border border-amber-300/30 whitespace-nowrap">
                                                        底牌展示中…
                                                    </div>
                                                </>
                                            ) : (
                                                <div className="px-2 py-0.5 rounded text-[10px] sm:text-xs text-amber-200/80 animate-pulse whitespace-nowrap">
                                                    翻底定庄中 · 已翻 {localFlippedCount}/{game.bottomCards.length}
                                                </div>
                                            )}
                                        </div>
                                        <div className="flex gap-1.5 sm:gap-2 justify-center" style={{ perspective: '800px' }}>
                                            {game.bottomCards.map((card, i) => {
                                                const flippedCount = localFlippedCount;
                                                const isFlipped = i < flippedCount;
                                                return (
                                                    <div
                                                        key={i}
                                                        className="w-10 h-14 sm:w-12 sm:h-16 relative"
                                                    >
                                                        <div
                                                            className="absolute inset-0"
                                                            style={{
                                                                transformStyle: 'preserve-3d',
                                                                transform: isFlipped ? 'rotateY(180deg)' : 'rotateY(0deg)',
                                                                transition: 'transform 1.5s ease-in-out',
                                                            }}
                                                        >
                                                            {/* 卡背 */}
                                                            <div
                                                                className="absolute inset-0 rounded-xl border-2 border-white/20 bg-blue-900/40 backdrop-blur-sm shadow-lg flex items-center justify-center"
                                                                style={{ backfaceVisibility: 'hidden', WebkitBackfaceVisibility: 'hidden' }}
                                                            >
                                                                <div className="w-[85%] h-[85%] rounded-md bg-gradient-to-br from-blue-600/70 to-blue-800/70 border border-blue-400/30 flex items-center justify-center">
                                                                    <span className="text-[8px] text-blue-200/60 font-bold">?</span>
                                                                </div>
                                                            </div>
                                                            {/* 卡面：用 PlayingCard 保证与手牌外观一致 */}
                                                            <div
                                                                className="absolute inset-0"
                                                                style={{
                                                                    backfaceVisibility: 'hidden',
                                                                    WebkitBackfaceVisibility: 'hidden',
                                                                    transform: 'rotateY(180deg)',
                                                                }}
                                                            >
                                                                <PlayingCard
                                                                    card={card}
                                                                    size="sm"
                                                                    isTrumpRank={game.trumpRank != null && card.value === game.trumpRank}
                                                                />
                                                            </div>
                                                        </div>
                                                    </div>
                                                );
                                            })}
                                        </div>
                                    </div>
                                    );
                                })()}

                                {/* 打牌阶段：5 行均分，每行与左侧对应玩家头像对齐 */}
                                {game.status === EGameStatus.PLAYING && (
                                    <div className="absolute inset-0 z-10 flex flex-col">
                                        {[1, 2, 3, 4, 5].map(seat => {
                                            const player = players.find(p => p.position === seat);
                                            const played = player ? game.currentTrick.find(t => t.playerId === player.id) : undefined;
                                            const lastPlayed = player && !played && game.lastCompletedTrick ? game.lastCompletedTrick.find(t => t.playerId === player.id) : undefined;
                                            const showCards = played || (showLastTrick && lastPlayed);
                                            const cardsToShow = played ? played.cards : (lastPlayed ? lastPlayed.cards : []);
                                            const biggestCard = getCurrentWinner();
                                            const isBiggest = played && biggestCard && biggestCard.playerId === played.playerId;
                                            return (
                                                <div key={seat} className="flex-1 min-h-0 flex items-center px-2 sm:px-3 gap-1 border-b border-white/5 last:border-b-0 relative">
                                                    {showCards && cardsToShow.length > 0 ? (
                                                        <>
                                                            {isBiggest && (
                                                                <div className="absolute left-1 top-1 px-1 py-0.5 rounded bg-yellow-400 text-[8px] sm:text-[10px] font-bold text-black z-10">最大</div>
                                                            )}
                                                            <div className={cn('flex gap-0.5 sm:gap-1 flex-wrap h-full items-center', !played && 'opacity-50')}>
                                                                {cardsToShow.map((card, j) => (
                                                                    <div key={j} className={cn(
                                                                        'h-full max-h-12 sm:max-h-16 aspect-[2/3] flex flex-col items-center justify-center rounded-md font-bold border bg-white/15 backdrop-blur-sm overflow-hidden',
                                                                        isBiggest ? 'border-yellow-400/60' : 'border-white/30'
                                                                    )}>
                                                                        {isJoker(card) ? (
                                                                            <img src={getJokerImageUrl(card)} alt={card.value === 'Big' ? '大王' : '小王'} className="w-full h-full object-contain" />
                                                                        ) : (
                                                                            <>
                                                                                <span className={cn(getSuitClass(card.suit), 'text-sm sm:text-lg leading-none font-black')}>{SUIT_SYMBOLS[card.suit]||''}</span>
                                                                                <span className={cn(getSuitClass(card.suit), 'text-[10px] sm:text-sm leading-none font-bold')}>{card.value}</span>
                                                                            </>
                                                                        )}
                                                                    </div>
                                                                ))}
                                                            </div>
                                                        </>
                                                    ) : null}
                                                </div>
                                            );
                                        })}
                                    </div>
                                )}

                                {/* 上一轮折叠按钮 */}
                                {game.status === EGameStatus.PLAYING && game.lastCompletedTrick && game.lastCompletedTrick.length > 0 && game.currentTrick.length === 0 && (
                                    <button
                                        onClick={() => setShowLastTrick(!showLastTrick)}
                                        className="absolute top-1 right-1 z-20 flex items-center gap-1 text-[9px] sm:text-xs text-white/50 hover:text-white/80 transition-colors px-1.5 py-0.5 rounded bg-black/30 border border-white/10"
                                    >
                                        <span className={cn('transition-transform', showLastTrick && 'rotate-90')}>▶</span>
                                        <span>上一轮</span>
                                    </button>
                                )}

                                {/* 等待状态提示 */}
                                {game.status === EGameStatus.WAITING && (
                                    <div className="absolute inset-0 flex items-center justify-center text-white/30 text-sm">等待开始...</div>
                                )}
                            </div>
                        </div>
                    </div>
                </div>
            </div>

            {/* ====== 全屏对话框（渲染在游戏桌之上）====== */}
            {/* 注意：亮庄/反庄面板不在此处，它已迁移到桌布外部、手牌区上方（见下方"亮庄面板"） */}

            {/* 扣牌对话框：以 portal 形式贴在牌垫上，尺寸与位置完全跟随牌垫。
                内嵌"翻底定庄"信息：级牌（trumpSuit + trumpRank）+ 庄家座位 + 底牌（默认展开、与手牌外观一致）。 */}
            {showDiscardDialog && cardMatEl && createPortal(
                <DiscardDialog
                    bottomCards={game.bottomCards}
                    onSubmit={handleDiscard}
                    isPending={discardMutation.isPending}
                    trumpSuit={game.trumpSuit}
                    trumpRank={game.trumpRank}
                    dealerSeat={game.dealerSeat}
                    myHand={game.myHand}
                    embedded
                />,
                cardMatEl,
            )}

            {/* 按钮层：以 portal 形式贴在牌垫上，承载"开始游戏"等悬浮按钮 */}
            {cardMatEl && game.status === EGameStatus.WAITING && isSinglePlayerRoute && createPortal(
                <div
                    className="absolute inset-0 z-30 flex flex-col items-center justify-center gap-3 rounded-xl bg-black/30 backdrop-blur-[2px] pointer-events-none"
                    aria-label="按钮层"
                >
                    <p className="text-xs sm:text-sm text-white/70 pointer-events-auto">
                        单人模式：随时点击开始游戏
                    </p>
                    <Button
                        variant="game"
                        size="default"
                        className="gap-1 sm:gap-2 text-sm sm:text-base font-bold pointer-events-auto"
                        onClick={() => startSinglePlayerMutation.mutate()}
                        disabled={startSinglePlayerMutation.isPending}
                    >
                        {startSinglePlayerMutation.isPending ? (
                            <Loader2 className="h-4 w-4 sm:h-5 sm:w-5 animate-spin" />
                        ) : (
                            <>
                                <Play className="h-4 w-4 sm:h-5 sm:w-5" />
                                开始游戏
                            </>
                        )}
                    </Button>
                </div>,
                cardMatEl,
            )}

            {/* 亮庄状态条：以 portal 形式贴在牌垫中央，
                显示窗口 = 发牌开始（dealing）→ 亮庄阶段结束（callPhase 离开 dealing/counting）
                - 发牌阶段（DEALING 或 CALLING.dealing）：有人亮庄时展示"谁用几张亮了什么"，无倒计时数字
                - 倒计时阶段（CALLING.counting）：同时展示倒计时数字
                - 亮庄定庄进入 discarding 后整个状态条消失（与翻底定庄互斥，不展示底牌） */}
            {cardMatEl
                && (game.status === EGameStatus.DEALING || game.status === EGameStatus.CALLING)
                && (game.callPhase === 'dealing' || game.callPhase === 'counting')
                && createPortal(
                <div
                    className="absolute inset-0 z-30 flex flex-col items-center justify-center gap-2 pointer-events-none pt-20 sm:pt-24"
                    aria-label="亮庄状态"
                >
                    {game.callPhase === 'counting' && localCountdown != null && localCountdown > 0 && (
                        <div className="px-4 py-2 rounded-full text-sm sm:text-base font-bold tracking-wide text-amber-100 bg-amber-900/70 border border-amber-300/50 backdrop-blur-sm shadow-lg">
                            亮庄倒计时 {localCountdown}s
                        </div>
                    )}
                    {(() => {
                        const lastCall = game.callRecords && game.callRecords.length > 0
                            ? game.callRecords[game.callRecords.length - 1]
                            : null;
                        if (lastCall) {
                            const caller = game.players?.find(p => p.position === lastCall.seat);
                            const suitSymbol = SUIT_SYMBOLS[lastCall.suit] || '';
                            const suitColor = lastCall.suit === 'hearts' || lastCall.suit === 'diamonds' ? 'text-red-400' : 'text-slate-100';
                            return (
                                <div className="px-3 py-1 rounded-full text-xs sm:text-sm text-white/80 bg-black/40 border border-white/20 backdrop-blur-sm">
                                    座位{lastCall.seat}{caller ? ` ${caller.username}` : ''} 亮庄：
                                    <span className={cn(suitColor, 'font-bold')}>{suitSymbol}</span>
                                    <span className="font-bold text-amber-300">{lastCall.rank}</span>
                                    ×{lastCall.count}
                                </div>
                            );
                        }
                        if (game.callPhase === 'counting') {
                            return (
                                <div className="px-3 py-1 rounded-full text-xs sm:text-sm text-white/50 bg-black/40 border border-white/20 backdrop-blur-sm">
                                    当前无人亮庄，倒计时结束后翻底牌定庄
                                </div>
                            );
                        }
                        return null;
                    })()}
                </div>,
                cardMatEl,
            )}

            {/* 翻底定庄公告：内容已合并到 DiscardDialog（扣底牌）中，不再独立展示。 */}

            {/* 叫朋友面板：以 portal 形式嵌入到牌垫中（与扣底牌对话框同样做法）。
                不再提供"隐藏/唤起"按钮——庄家需要直接在牌垫上完成选择并提交。 */}
            {showCallFriendDialog && cardMatEl && createPortal(
                <CallFriendDialog
                    onSubmit={handleCallFriend}
                    isPending={callFriendMutation.isPending}
                    currentLevel={game.currentLevel}
                    embedded
                />,
                cardMatEl,
            )}

            {/* 亮庄 / 反庄面板：桌布外部下方、手牌区上方
                显示窗口 = "从发牌开始" 到 "亮庄阶段结束"：
                - 发牌阶段（dealing）：玩家可亮庄/反庄，但不倒计时
                - 倒计时阶段（counting）：玩家可亮庄/反庄，倒计时进行中
                - 翻底牌阶段（flipping）及以后：与亮庄/反庄互斥，本面板消失，
                  由"翻底定庄"动画接管（详见 rules/03-bidding.md §3.4）
                - 定庄完成（finished）/扣底（discarding）：本面板消失 */}
            {(game.status === EGameStatus.DEALING || game.status === EGameStatus.CALLING)
                && (game.callPhase === 'dealing' || game.callPhase === 'counting')
                && (() => {
                    const mySeat = game.myPosition;
                    const hasCalled = game.callRecords?.some(r => r.seat === mySeat);
                    const hasPassed = game.passedSeats?.includes(mySeat);
                    const showPass = !hasCalled && !hasPassed;
                    return (
                        <div className="px-2 mt-1">
                            <CallDealerDialog
                                onSubmit={handleCallDealer}
                                isPending={callDealerMutation.isPending}
                                currentLevel={
                                    game.players?.find(p => p.position === game.myPosition)?.level
                                    || game.currentLevel
                                    || '2'
                                }
                                myHand={game.myHand}
                                players={game.players}
                                myPosition={game.myPosition}
                                callRecords={game.callRecords}
                                showPassButton={showPass}
                                onPass={() => passCallMutation.mutate()}
                                isPassPending={passCallMutation.isPending}
                            />
                        </div>
                    );
                })()}

            {/* 手牌区域 */}
            <div className="border-t border-white/10 hand-area-glass">
                <div className="sm:p-3 pt-4 overflow-hidden">
                    <PlayerHand
                        cards={game.myHand}
                        trumpRank={game.trumpRank}
                        trumpSuit={game.trumpSuit ?? undefined}
                        gameStatus={game.status}
                        friendCard={game.hostCalledCard}
                        size={game.myHand?.length > 20 ? "xs" : "sm"}
                        myCalledRank={(() => {
                          const myRecord = game.callRecords?.find(r => r.seat === game.myPosition);
                          return myRecord?.rank;
                        })()}
                        otherCalledRank={(() => {
                          const otherRecords = game.callRecords?.filter(r => r.seat !== game.myPosition);
                          return otherRecords?.length ? otherRecords[otherRecords.length - 1].rank : undefined;
                        })()}
                    />
                </div>

                {/* 游戏控制按钮 */}
                <div className="mt-2 sm:mt-3 rounded-xl border border-white/15 game-control-panel p-2 sm:p-4">
                    {game.status === EGameStatus.WAITING && !isSinglePlayerRoute && (
                        <div className="flex flex-col items-center gap-2 sm:gap-3">
                            <div className="text-center">
                                <p className="text-xs sm:text-sm text-white/70">
                                    等待玩家准备... ({readyStatusData?.readyStates?.filter(s => s.isReady).length || 0}/5 已准备)
                                </p>
                                <div className="mt-1 sm:mt-2 flex flex-wrap justify-center gap-1 sm:gap-2">
                                    {players?.map((player) => {
                                        const readyState = readyStatusData?.readyStates?.find(s => s.userId === String(player.id));
                                        const isReady = readyState?.isReady || false;
                                        return (
                                            <div
                                                key={player.id}
                                                className={`rounded-full px-2 sm:px-3 py-0.5 sm:py-1 text-xs sm:text-sm backdrop-blur-sm transition-all ${
                                                    isReady
                                                        ? 'bg-emerald-500/25 border border-emerald-400/40 text-emerald-200'
                                                        : 'bg-white/10 border border-white/20 text-white/70'
                                                }`}
                                            >
                                                {player.username}
                                                {player.id === Number(user?.id) && ' (你)'}
                                                {isReady && ' ✓'}
                                            </div>
                                        );
                                    })}
                                </div>
                            </div>

                            {/* 准备/取消准备按钮 */}
                            {(() => {
                                const myReadyState = readyStatusData?.readyStates?.find(s => s.userId === user?.id);
                                const amIReady = myReadyState?.isReady ?? false;

                                return amIReady ? (
                                    <Button
                                        variant="outline"
                                        size="default"
                                        className="gap-1 sm:gap-2 text-sm sm:text-base border-white/25 text-white/80 hover:bg-white/10 hover:text-white w-full sm:w-auto backdrop-blur-sm"
                                        onClick={() => cancelReadyMutation.mutate()}
                                        disabled={cancelReadyMutation.isPending}
                                    >
                                        {cancelReadyMutation.isPending ? (
                                            <Loader2 className="h-4 w-4 sm:h-5 sm:w-5 animate-spin" />
                                        ) : (
                                            '取消准备'
                                        )}
                                    </Button>
                                ) : (
                                    <Button
                                        variant="game"
                                        size="default"
                                        className="gap-1 sm:gap-2 text-sm sm:text-base font-bold w-full sm:w-auto"
                                        onClick={() => readyMutation.mutate()}
                                        disabled={readyMutation.isPending}
                                    >
                                        {readyMutation.isPending ? (
                                            <Loader2 className="h-4 w-4 sm:h-5 sm:w-5 animate-spin" />
                                        ) : (
                                            <>
                                                <Play className="h-4 w-4 sm:h-5 sm:w-5" />
                                                准备
                                            </>
                                        )}
                                    </Button>
                                );
                            })()}

                            {(players?.length || 0) < 5 && (
                                <p className="text-[10px] sm:text-xs text-white/40 text-center px-1">
                                    需要5人加入并全部准备后开始游戏
                                </p>
                            )}
                            {(players?.length || 0) >= 5 && (
                                <p className="text-[10px] sm:text-xs text-white/40 text-center px-1">
                                    所有玩家准备后游戏将自动开始
                                </p>
                            )}
                        </div>
                    )}

                    {/* 发牌状态 */}
                    {game.status === EGameStatus.DEALING && (
                        <div className="flex flex-col items-center gap-3">
                            <div className="text-center">
                                <p className="text-lg font-bold text-white/90">发牌中...</p>
                                <p className="text-sm text-white/60">
                                    已发 {Math.ceil((game.dealtCardCount || 0) / 5)}/{game.totalCardsPerPlayer || 31} 张/人
                                    <span className="ml-2 text-white/40">
                                        （总计 {game.dealtCardCount || 0}/{(game.totalCardsPerPlayer || 31) * 5} 张）
                                    </span>
                                </p>
                                <div className="deal-progress-bar mt-2 w-64">
                                    <div
                                        className="deal-progress-fill"
                                        style={{ width: `${((game.dealtCardCount || 0) / ((game.totalCardsPerPlayer || 31) * 5)) * 100}%` }}
                                    />
                                </div>
                            </div>

                            {/* 发牌过程中即可亮庄/反庄；倒计时只在发完牌后启动（详见 rules/03-bidding.md §3.0） */}
                            <p className="text-xs text-white/40">
                                可以在发牌过程中选择级牌进行亮庄/反庄
                            </p>
                        </div>
                    )}

                    <div className="flex flex-col gap-2 sm:gap-3">
                        {/* "不叫庄" 按钮已移至上方 CallDealerDialog 同一行，此处不再渲染 */}
                        {/* 扣底牌阶段不再在底部一行显示按钮/等待提示：扣牌已统一在牌垫上方的浮动对话框层呈现 */}
                        {/* 叫朋友阶段同理：叫朋友交互统一在浮动对话框层呈现，底部一行不再展示 */}
                        {game.status === EGameStatus.PLAYING && game.currentPlayer === game.myPosition && (
                            <>
                                <Button
                                    variant="game"
                                    size="default"
                                    className="gap-1 sm:gap-2 text-sm sm:text-base font-bold w-full sm:w-auto"
                                    onClick={handlePlay}
                                    disabled={selectedCardIndices.size === 0 || playCards.isPending}
                                >
                                    {playCards.isPending ? <Loader2 className="h-4 w-4 sm:h-5 sm:w-5 animate-spin" /> : <Play className="h-4 w-4 sm:h-5 sm:w-5" />}
                                    出牌 ({selectedCardIndices.size})
                                </Button>
                            </>
                        )}
                        {game.status === EGameStatus.PLAYING && game.currentPlayer !== game.myPosition && (
                            <div className="waiting-glass rounded-xl p-2 sm:p-4 text-center border-blue-400/20">
                                <p className="text-xs sm:text-sm text-blue-200">等待其他玩家出牌...</p>
                            </div>
                        )}
                    </div>
                    {selectedCardIndices.size > 0 && game.status === EGameStatus.PLAYING && (
                        <p className="mt-2 text-center text-xs sm:text-sm text-amber-400 px-2">
                            已选择 {selectedCardIndices.size} 张牌，点击"出牌"按钮进行出牌
                        </p>
                    )}
                </div>
                {playCards.isError && (
                    <p className="mt-2 text-center text-sm text-destructive">{playCards.error.message}</p>
                )}
                {callDealerMutation.isError && (
                    <p className="mt-2 text-center text-sm text-destructive">{callDealerMutation.error.message}</p>
                )}
                {discardMutation.isError && (
                    <p className="mt-2 text-center text-sm text-destructive">{discardMutation.error.message}</p>
                )}
                {callFriendMutation.isError && (
                    <p className="mt-2 text-center text-sm text-destructive">{callFriendMutation.error.message}</p>
                )}
                {aiPlay.isError && (
                    <p className="mt-2 text-center text-sm text-destructive">AI 出牌失败：{aiPlay.error.message}</p>
                )}
            </div>
        </div>
    );
}
