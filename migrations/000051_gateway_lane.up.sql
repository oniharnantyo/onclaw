-- Gateway lane (add-whatsapp-gateway design D1): the explicit discriminator
-- between a platform's connection modes. WhatsApp rows carry 'cloud_api' or
-- 'multi_device'; Telegram rows keep lane NULL — lane is never inferred from
-- credential shape. Nullable and unbackfilled: every existing row is
-- Telegram, whose validation ignores the lane entirely.

ALTER TABLE workspace_gateways
    ADD COLUMN lane text;
