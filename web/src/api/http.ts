import axios, { AxiosError } from "axios";

import type { ApiFailure } from "./types";

export const currentShopId = { value: null as number | null };

export const http = axios.create({
  baseURL: "/api/v1",
  timeout: 60000,
});

http.interceptors.request.use((config) => {
  if (currentShopId.value) {
    config.headers["X-Shop-Id"] = String(currentShopId.value);
  }
  return config;
});

export function apiError(error: unknown): ApiFailure {
  if (error instanceof AxiosError) {
    const data = error.response?.data as
      | { message?: string; fields?: Record<string, string>; snapshot?: ApiFailure["snapshot"] }
      | undefined;
    if (data && typeof data.message === "string") {
      return {
        message: data.message,
        fields: data.fields,
        snapshot: data.snapshot,
        status: error.response?.status,
      };
    }
    if (error.code === "ECONNABORTED") {
      return { message: "请求超时，请稍后重试。" };
    }
    return { message: "无法连接本地工作台服务。" };
  }
  return { message: "请求失败。" };
}
