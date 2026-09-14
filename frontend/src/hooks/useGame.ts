import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { useNavigate } from 'react-router-dom';
import { useState, useEffect } from 'react';
import * as gameService from '@/services/game';
import type { IPlayCardRequest } from '@/types';

export function useGameList(wsConnected = false) {
  const [isActive, setIsActive] = useState(true);

  useEffect(() => {
    let timeoutId: number;

    const handleActivity = () => {
      setIsActive(true);
      clearTimeout(timeoutId);
      timeoutId = window.setTimeout(() => setIsActive(false), 30000);
    };

    // 初始触发
    handleActivity();

    window.addEventListener('mousemove', handleActivity);
    window.addEventListener('keydown', handleActivity);
    window.addEventListener('click', handleActivity);
    window.addEventListener('scroll', handleActivity);

    return () => {
      window.removeEventListener('mousemove', handleActivity);
      window.removeEventListener('keydown', handleActivity);
      window.removeEventListener('click', handleActivity);
      window.removeEventListener('scroll', handleActivity);
      clearTimeout(timeoutId);
    };
  }, []);

  return useQuery({
    queryKey: ['games'],
    queryFn: () => gameService.listGames(),
    refetchInterval: wsConnected ? 30000 : isActive ? 5000 : 15000,
    staleTime: 3000,
  });
}

export function useGameTable(gameId: string) {
  return useQuery({
    queryKey: ['gameTable', gameId],
    queryFn: () => gameService.getGameTable(gameId),
    refetchInterval: 1000,
    enabled: Boolean(gameId),
  });
}

export function useGame(gameId: string) {
  return useQuery({
    queryKey: ['game', gameId],
    queryFn: () => gameService.getGame(gameId),
    refetchInterval: 5000,
    enabled: Boolean(gameId),
  });
}

export function useCreateGame() {
  const navigate = useNavigate();

  return useMutation({
    mutationFn: (name: string) => gameService.createGame(name),
    onSuccess: (res) => {
      const gameId = res.gameId ?? res.game?.id;
      if (res.success && gameId) {
        navigate(`/game/table/${gameId}`);
      }
    },
  });
}

export function useCreateSinglePlayerGame() {
  const navigate = useNavigate();

  return useMutation({
    mutationFn: () => gameService.createSinglePlayerGame(),
    onSuccess: (res) => {
      const gameId = res.gameId ?? res.game?.id;
      if (res.success && gameId) {
        navigate(`/game/singleplayer/${gameId}`);
      }
    },
  });
}

export function useJoinGame() {
  const queryClient = useQueryClient();
  const navigate = useNavigate();

  return useMutation({
    mutationFn: (gameId: string) => gameService.joinGame(gameId),
    onSuccess: (_res, gameId) => {
      queryClient.invalidateQueries({ queryKey: ['game', gameId] });
      navigate(`/game/table/${gameId}`);
    },
  });
}

export function useStartGame() {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: (gameId: string) => gameService.startGame(gameId),
    onSuccess: (_res, gameId) => {
      queryClient.invalidateQueries({ queryKey: ['gameTable', gameId] });
    },
  });
}

export function useStartSinglePlayerGame(gameId: string) {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: () => gameService.startSinglePlayerGame(gameId),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['gameTable', gameId] });
      queryClient.invalidateQueries({ queryKey: ['game', gameId] });
    },
  });
}

export function usePlayCards(gameId: string) {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: (data: IPlayCardRequest) => gameService.playCards(gameId, data),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['gameTable', gameId] });
    },
  });
}

export function useAiPlay(gameId: string) {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: () => gameService.aiPlay(gameId),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['gameTable', gameId] });
    },
  });
}

export function useGameReplay(gameId: string) {
  return useQuery({
    queryKey: ['gameReplay', gameId],
    queryFn: () => gameService.getGameReplay(gameId),
    enabled: Boolean(gameId),
  });
}

export function useGameActions(gameId: string) {
  return useQuery({
    queryKey: ['gameActions', gameId],
    queryFn: () => gameService.getGameActions(gameId),
    enabled: Boolean(gameId),
  });
}

export function useCallDealer(gameId: string) {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: (params: { cardIndices: number[] }) =>
      gameService.callDealer(gameId, params.cardIndices),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['gameTable', gameId] });
    },
  });
}

export function usePassCall(gameId: string) {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: () => gameService.passCall(gameId),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['gameTable', gameId] });
    },
  });
}

export function useDiscardBottomCards(gameId: string) {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: (cardIndices: number[]) =>
      gameService.discardBottomCards(gameId, cardIndices),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['gameTable', gameId] });
    },
  });
}

export function useCallFriend(gameId: string) {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: (params: { suit: string; value: string; position: number }) =>
      gameService.callFriend(gameId, params.suit, params.value, params.position),
    onSuccess: (data) => {
      // 立即更新缓存中的游戏表格数据（包含 game 和 table）
      if (data?.game || data?.table) {
        queryClient.setQueryData(['gameTable', gameId], {
          ...data,
          success: true,
        });
      }
      // 同时触发一次 invalidate 确保数据一致性
      queryClient.invalidateQueries({ queryKey: ['gameTable', gameId] });
    },
  });
}

// 准备相关hooks
export function usePlayerReady(gameId: string) {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: () => gameService.setPlayerReady(gameId),
    onSuccess: (data) => {
      queryClient.invalidateQueries({ queryKey: ['gameTable', gameId] });
      queryClient.invalidateQueries({ queryKey: ['readyStatus', gameId] });
      // 如果游戏开始了，也刷新游戏状态
      if (data.gameStarted) {
        queryClient.invalidateQueries({ queryKey: ['game', gameId] });
      }
    },
  });
}

export function useCancelReady(gameId: string) {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: () => gameService.cancelPlayerReady(gameId),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['gameTable', gameId] });
      queryClient.invalidateQueries({ queryKey: ['readyStatus', gameId] });
    },
  });
}

export function useReadyStatus(gameId: string) {
  return useQuery({
    queryKey: ['readyStatus', gameId],
    queryFn: () => gameService.getReadyStatus(gameId),
    refetchInterval: 2000, // 每2秒刷新一次
    enabled: !!gameId,
  });
}

// 发牌相关hook
export function useDealNextCard(gameId: string) {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: () => gameService.dealNextCard(gameId),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['gameTable', gameId] });
    },
  });
}

// 获取玩家已出牌历史
export function usePlayerPlayedCards(gameId: string, enabled = false) {
  return useQuery({
    queryKey: ['playerPlayedCards', gameId],
    queryFn: () => gameService.getPlayerPlayedCards(gameId),
    enabled: Boolean(gameId) && enabled,
    staleTime: 0, // 每次打开对话框都重新获取
  });
}

// 开始新的一局
export function useNextRound(gameId: string) {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: () => gameService.nextRound(gameId),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['gameTable', gameId] });
    },
  });
}
