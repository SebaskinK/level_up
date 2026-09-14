import { get, post } from '@/lib/request';
import type {
  ICreateGameResponse,
  IGameResponse,
  IPlayCardRequest,
} from '@/types';

export const listGames = () =>
  get<{ success: boolean; games: unknown[] }>('/games');

export const createGame = (name: string) =>
  post<ICreateGameResponse>('/game/create', { name });

export const createSinglePlayerGame = () =>
  post<ICreateGameResponse>('/game/singleplayer');

export const getGame = (id: string) => get<IGameResponse>(`/game/${id}`);

export const getGameTable = (id: string) => get<IGameResponse>(`/game/${id}/table`);

export const joinGame = (id: string) =>
  post<{ success: boolean; error?: string }>(`/game/${id}/join`);

export const startGame = (id: string) =>
  post<{ success: boolean; error?: string }>(`/game/${id}/start`);

export const startSinglePlayerGame = (id: string) =>
  post<IGameResponse>(`/game/${id}/start-single`);

export const callFriend = (id: string, suit: string, value: string, position = 1) =>
  post<IGameResponse>(`/game/${id}/call-friend`, { suit, value, position });

export const callDealer = (id: string, cardIndices: number[]) =>
  post<IGameResponse>(`/game/${id}/call-dealer`, { cardIndices });

export const passCall = (id: string) =>
  post<IGameResponse>(`/game/${id}/pass-call`, {});

export const flipBottomCard = (id: string) =>
  post<IGameResponse>(`/game/${id}/flip-bottom`);

export const discardBottomCards = (id: string, cardIndices: number[]) =>
  post<IGameResponse>(`/game/${id}/discard-bottom`, { cardIndices });

export const playCards = (id: string, data: IPlayCardRequest) =>
  post<IGameResponse>(`/game/${id}/play`, data);

export const aiPlay = (id: string) =>
  post<IGameResponse>(`/game/${id}/ai-play`);

export const getGameReplay = (id: string) =>
  get<{ success: boolean; replay?: unknown }>(`/game/${id}/replay`);

export const getGameActions = (id: string) =>
  get<{ success: boolean; actions?: unknown[] }>(`/game/${id}/actions`);

// 准备相关API
export interface IReadyState {
  userId: string;
  username: string;
  seat: number;
  isReady: boolean;
}

export interface IReadyResponse {
  success: boolean;
  message?: string;
  gameStarted?: boolean;
  table?: unknown;
  readyStates?: IReadyState[];
  totalPlayers?: number;
}

export const setPlayerReady = (id: string) =>
  post<IReadyResponse>(`/game/${id}/ready`);

export const cancelPlayerReady = (id: string) =>
  post<IReadyResponse>(`/game/${id}/cancel-ready`);

export const getReadyStatus = (id: string) =>
  get<{ success: boolean; readyStates: IReadyState[]; totalPlayers: number; allReady: boolean }>(`/game/${id}/ready-status`);

// 发牌相关API
export interface IDealNextResponse {
  success: boolean;
  table?: unknown;
  complete: boolean;
  dealtCardCount: number;
  totalCardsPerPlayer: number;
}

export const dealNextCard = (id: string) =>
  post<IDealNextResponse>(`/game/${id}/deal-next`);

// 获取玩家已出牌历史
export interface IPlayedCardRecord {
  id: number;
  gameId: string;
  actionType: string;
  playerSeat: number;
  playerId: string;
  actionData: {
    card_indices: number[];
    cards: Array<{
      suit: string;
      value: string;
    }>;
    is_lead: boolean;
    play_type: string;
  };
  resultData: {
    success: boolean;
  };
  timestamp: string;
}

export interface IPlayerPlayedCardsResponse {
  success: boolean;
  playedCards: IPlayedCardRecord[];
  count: number;
  playerSeat: number;
}

export const getPlayerPlayedCards = (id: string) =>
  get<IPlayerPlayedCardsResponse>(`/game/${id}/played-cards`);

// 开始新的一局
export interface INextRoundResponse {
  success: boolean;
  message?: string;
  table?: unknown;
}

export const nextRound = (id: string) =>
  post<INextRoundResponse>(`/game/${id}/next-round`);
