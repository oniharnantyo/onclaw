import { describe, it, expect } from 'vitest';
import {
  telegramGatewayStatusView,
  whatsappGatewayStatusView,
  defaultGatewayPlatform,
  formatCountdown,
  maskWhatsAppId,
} from './gateways';
import type { ApiGatewayConfig, ApiWhatsAppGatewayConfig, ApiWhatsAppHealth } from './api';

describe('lib/gateways status mapping and helpers', () => {
  describe('telegramGatewayStatusView', () => {
    it('returns "Not set up" with muted dot when unconfigured', () => {
      expect(telegramGatewayStatusView(null)).toEqual({
        label: 'Not set up',
        dot: 'bg-muted',
        dotColor: 'muted',
        errored: false,
        state: 'unconfigured',
      });
      expect(
        telegramGatewayStatusView({
          token_hint: null,
          bot_username: null,
        } as unknown as ApiGatewayConfig)
      ).toEqual({
        label: 'Not set up',
        dot: 'bg-muted',
        dotColor: 'muted',
        errored: false,
        state: 'unconfigured',
      });
    });

    it('returns "Error" with danger dot when status_error is present', () => {
      const res = telegramGatewayStatusView({
        token_hint: '1234',
        bot_username: 'my_bot',
        enabled: true,
        status_error: 'Conflict: terminated by other getUpdates request',
      } as unknown as ApiGatewayConfig);
      expect(res).toEqual({
        label: 'Error',
        dot: 'bg-danger',
        dotColor: 'danger',
        errored: true,
        state: 'error',
      });
    });

    it('returns "Paused" with muted dot when configured but disabled', () => {
      const res = telegramGatewayStatusView({
        token_hint: '1234',
        bot_username: 'my_bot',
        enabled: false,
        status_error: null,
      } as unknown as ApiGatewayConfig);
      expect(res).toEqual({
        label: 'Paused',
        dot: 'bg-muted',
        dotColor: 'muted',
        errored: false,
        state: 'paused',
      });
    });

    it('returns "Connected" with success dot when configured and enabled', () => {
      const res = telegramGatewayStatusView({
        token_hint: '1234',
        bot_username: 'my_bot',
        enabled: true,
        status_error: null,
      } as unknown as ApiGatewayConfig);
      expect(res).toEqual({
        label: 'Connected',
        dot: 'bg-success',
        dotColor: 'accent',
        errored: false,
        state: 'connected',
      });
    });
  });

  describe('whatsappGatewayStatusView', () => {
    it('returns "Not set up" with muted dot when unconfigured', () => {
      expect(whatsappGatewayStatusView(null, null)).toEqual({
        label: 'Not set up',
        dot: 'bg-muted',
        dotColor: 'muted',
        errored: false,
        state: 'unconfigured',
      });
      expect(
        whatsappGatewayStatusView(
          { lane: null, has_credentials: false } as unknown as ApiWhatsAppGatewayConfig,
          null
        )
      ).toEqual({
        label: 'Not set up',
        dot: 'bg-muted',
        dotColor: 'muted',
        errored: false,
        state: 'unconfigured',
      });
    });

    it('returns "Paused" with muted dot when configured but disabled', () => {
      const res = whatsappGatewayStatusView(
        { lane: 'cloud_api', has_credentials: true, enabled: false } as unknown as ApiWhatsAppGatewayConfig,
        { status: 'ok' } as ApiWhatsAppHealth
      );
      expect(res).toEqual({
        label: 'Paused',
        dot: 'bg-muted',
        dotColor: 'muted',
        errored: false,
        state: 'paused',
      });
    });

    it('returns "Error" with danger dot when health status is error', () => {
      const res = whatsappGatewayStatusView(
        { lane: 'cloud_api', has_credentials: true, enabled: true } as unknown as ApiWhatsAppGatewayConfig,
        { status: 'error', detail: 'Invalid token' } as ApiWhatsAppHealth
      );
      expect(res).toEqual({
        label: 'Error',
        dot: 'bg-danger',
        dotColor: 'danger',
        errored: true,
        state: 'error',
      });
    });

    it('returns "Connected" for cloud_api when enabled and health is ok', () => {
      const res = whatsappGatewayStatusView(
        { lane: 'cloud_api', has_credentials: true, enabled: true } as unknown as ApiWhatsAppGatewayConfig,
        { status: 'ok' } as ApiWhatsAppHealth
      );
      expect(res).toEqual({
        label: 'Connected',
        dot: 'bg-success',
        dotColor: 'accent',
        errored: false,
        state: 'connected',
      });
    });

    it('returns "Linked" for multi_device when enabled and health is ok', () => {
      const res = whatsappGatewayStatusView(
        { lane: 'multi_device', has_credentials: false, enabled: true } as unknown as ApiWhatsAppGatewayConfig,
        { status: 'ok' } as ApiWhatsAppHealth
      );
      expect(res).toEqual({
        label: 'Linked',
        dot: 'bg-success',
        dotColor: 'accent',
        errored: false,
        state: 'linked',
      });
    });
  });

  describe('defaultGatewayPlatform', () => {
    it('prioritizes Error over configured over unconfigured', () => {
      const tgError = telegramGatewayStatusView({ enabled: true, status_error: 'bad token', token_hint: '1234' } as any);
      const tgOk = telegramGatewayStatusView({ enabled: true, bot_username: 'bot' } as any);
      const tgUnconfigured = telegramGatewayStatusView(null);
      const tgPaused = telegramGatewayStatusView({ enabled: false, bot_username: 'bot' } as any);

      const waError = whatsappGatewayStatusView({ lane: 'cloud_api', enabled: true } as any, { status: 'error' } as any);
      const waOk = whatsappGatewayStatusView({ lane: 'cloud_api', enabled: true } as any, { status: 'ok' } as any);
      const waUnconfigured = whatsappGatewayStatusView(null, null);

      // Telegram error wins
      expect(defaultGatewayPlatform(tgError, waError)).toBe('telegram');
      expect(defaultGatewayPlatform(tgError, waUnconfigured)).toBe('telegram');

      // WhatsApp error wins if TG has no error
      expect(defaultGatewayPlatform(tgOk, waError)).toBe('whatsapp');
      expect(defaultGatewayPlatform(tgPaused, waError)).toBe('whatsapp');
      expect(defaultGatewayPlatform(tgUnconfigured, waError)).toBe('whatsapp');

      // Configured priority
      expect(defaultGatewayPlatform(tgOk, waUnconfigured)).toBe('telegram');
      expect(defaultGatewayPlatform(tgPaused, waUnconfigured)).toBe('telegram');
      expect(defaultGatewayPlatform(tgUnconfigured, waOk)).toBe('whatsapp');

      // Both unconfigured -> first (Telegram)
      expect(defaultGatewayPlatform(tgUnconfigured, waUnconfigured)).toBe('telegram');

      // Both healthy / paused -> first configured (Telegram)
      expect(defaultGatewayPlatform(tgOk, waOk)).toBe('telegram');
      expect(defaultGatewayPlatform(tgPaused, waOk)).toBe('telegram');
    });
  });

  describe('maskWhatsAppId', () => {
    it('masks phone numbers preserving country code and last 2 digits', () => {
      expect(maskWhatsAppId('6281234567890')).toBe('+62•••••••••90');
      expect(maskWhatsAppId('+14155552671')).toBe('+14•••••••71');
      expect(maskWhatsAppId('1234')).toBe('+1234');
      expect(maskWhatsAppId('')).toBe('');
      expect(maskWhatsAppId(null)).toBe('');
    });
  });

  describe('formatCountdown', () => {
    it('formats remaining time into mm:ss', () => {
      const now = 1000000;
      expect(formatCountdown(new Date(now + 90000).toISOString(), now)).toBe('01:30');
      expect(formatCountdown(new Date(now - 1000).toISOString(), now)).toBe('00:00');
    });
  });
});
