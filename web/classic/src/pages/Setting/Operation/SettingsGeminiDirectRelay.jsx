/*
Copyright (C) 2025 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/

import React, { useEffect, useRef, useState } from 'react';
import { Button, Form, Row, Col, Spin, Typography } from '@douyinfe/semi-ui';
import {
  API,
  compareObjects,
  showError,
  showSuccess,
  showWarning,
  toBoolean,
} from '../../../helpers';
import { useTranslation } from 'react-i18next';

const { Text } = Typography;

export default function SettingsGeminiDirectRelay({ options, refresh }) {
  const { t } = useTranslation();
  const [loading, setLoading] = useState(false);
  const defaults = {
    GeminiDirectRelayEnabled: false,
    GeminiDirectRelayGlobalEnabled: false,
    GeminiDirectTicketTTLSeconds: 600,
    GeminiDirectCallbackGraceSeconds: 1800,
  };
  const [inputs, setInputs] = useState(defaults);
  const [original, setOriginal] = useState(defaults);
  const formRef = useRef();
  useEffect(() => {
    const next = {
      GeminiDirectRelayEnabled: toBoolean(options?.GeminiDirectRelayEnabled),
      GeminiDirectRelayGlobalEnabled: toBoolean(
        options?.GeminiDirectRelayGlobalEnabled,
      ),
      GeminiDirectTicketTTLSeconds: Number(
        options?.GeminiDirectTicketTTLSeconds || 600,
      ),
      GeminiDirectCallbackGraceSeconds: Number(
        options?.GeminiDirectCallbackGraceSeconds || 1800,
      ),
    };
    setInputs(next);
    setOriginal(next);
    formRef.current?.setValues(next);
  }, [options]);
  const update = (key) => (value) =>
    setInputs((old) => ({ ...old, [key]: value }));
  const submit = () => {
    if (
      !Number.isInteger(inputs.GeminiDirectTicketTTLSeconds) ||
      !Number.isInteger(inputs.GeminiDirectCallbackGraceSeconds) ||
      inputs.GeminiDirectTicketTTLSeconds < 60 ||
      inputs.GeminiDirectTicketTTLSeconds > 3600 ||
      inputs.GeminiDirectCallbackGraceSeconds < 300 ||
      inputs.GeminiDirectCallbackGraceSeconds > 86400
    )
      return showError(t('直连超时时间不在允许范围内'));
    const changes = compareObjects(original, inputs);
    if (!changes.length) return showWarning(t('你似乎并没有修改什么'));
    setLoading(true);
    Promise.allSettled(
      changes.map(({ key }) =>
        API.put('/api/option/', { key, value: String(inputs[key]) }),
      ),
    )
      .then((results) => {
        const succeeded = results.filter(
          (result) =>
            result.status === 'fulfilled' &&
            result.value?.data?.success !== false,
        ).length;
        if (succeeded === 0) {
          showError(t('保存失败，请重试'));
          return;
        }
        if (succeeded !== results.length) {
          showError(t('部分保存失败，请重试'));
          refresh();
          return;
        }
        showSuccess(t('保存成功'));
        refresh();
      })
      .catch(() => showError(t('保存失败，请重试')))
      .finally(() => setLoading(false));
  };
  return (
    <Spin spinning={loading}>
      <Form values={inputs} getFormApi={(api) => (formRef.current = api)}>
        <Form.Section text={t('Gemini 图像客户端直连')}>
          <Row gutter={16}>
            <Col xs={24} sm={12} md={8}>
              <Form.Switch
                field='GeminiDirectRelayEnabled'
                label={t('启用 Gemini 图像客户端直连')}
                extraText={t('关闭或条件不满足时自动使用服务器中转')}
                onChange={update('GeminiDirectRelayEnabled')}
              />
            </Col>
            <Col xs={24} sm={12} md={8}>
              <Form.Switch
                field='GeminiDirectRelayGlobalEnabled'
                label={t('启用 Gemini 全局直连')}
                extraText={t(
                  '开启后，在服务端总开关启用的前提下，无需在用户编辑页单独启用直连，所有符合要求的用户都会使用直连模式',
                )}
                onChange={update('GeminiDirectRelayGlobalEnabled')}
              />
            </Col>
            <Col xs={24} sm={12} md={8}>
              <Form.InputNumber
                field='GeminiDirectTicketTTLSeconds'
                label={t('直连票据有效期（秒）')}
                min={60}
                max={3600}
                onChange={update('GeminiDirectTicketTTLSeconds')}
              />
            </Col>
            <Col xs={24} sm={12} md={8}>
              <Form.InputNumber
                field='GeminiDirectCallbackGraceSeconds'
                label={t('回调退款宽限期（秒）')}
                min={300}
                max={86400}
                onChange={update('GeminiDirectCallbackGraceSeconds')}
              />
            </Col>
          </Row>
          <Text type='tertiary'>
            {t(
              '仅支持原生 Gemini 非流式 generateContent 和无服务端 Proxy 的图像模型；不支持流式、Imagen predict、Vertex Service Account。关闭全局直连时，管理员还需要在用户编辑页为具体用户启用直连。客户端会直接使用上游 API Key，回调失败会重试，超时未回调会退款。',
            )}
          </Text>
          <Text type='tertiary' style={{ display: 'block', marginTop: 8 }}>
            {t(
              'Base64URL 仅用于轻度隐藏，不是加密；参考图上传仍会经过服务器。',
            )}
          </Text>
          <Text type='tertiary' style={{ display: 'block', marginTop: 8 }}>
            {t(
              '包含 param() 的 tiered billing 会自动回退服务器中转；上游成功但回调丢失可能发生退款。',
            )}
          </Text>
          <Row style={{ marginTop: 12 }}>
            <Button onClick={submit}>{t('保存 Gemini 直连设置')}</Button>
          </Row>
        </Form.Section>
      </Form>
    </Spin>
  );
}
