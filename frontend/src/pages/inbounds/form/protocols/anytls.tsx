import { useTranslation } from 'react-i18next';
import { Input, Select, Switch } from 'antd';
import { useFormContext, useWatch } from 'react-hook-form';

import { FormField } from '@/components/form/rhf';
import { useOutboundTags } from '@/api/queries/useOutboundTags';

export default function AnytlsFields() {
  const { t } = useTranslation();
  const { control } = useFormContext();
  const routeThroughXray = useWatch({ control, name: 'settings.routeThroughXray' }) as
    | boolean
    | undefined;
  const { data: outboundTags } = useOutboundTags();
  return (
    <>
      <FormField
        name={['settings', 'sni']}
        label={t('pages.inbounds.form.anytlsSni')}
        tooltip={t('pages.inbounds.form.anytlsSniHint')}
      >
        <Input placeholder="example.com" />
      </FormField>
      <FormField
        name={['settings', 'certFile']}
        label={t('pages.inbounds.form.anytlsCertFile')}
        tooltip={t('pages.inbounds.form.anytlsCertHint')}
      >
        <Input allowClear placeholder="/root/cert/fullchain.pem" />
      </FormField>
      <FormField name={['settings', 'keyFile']} label={t('pages.inbounds.form.anytlsKeyFile')}>
        <Input allowClear placeholder="/root/cert/privkey.pem" />
      </FormField>
      <FormField
        name={['settings', 'forward']}
        label={t('pages.inbounds.form.anytlsForward')}
        tooltip={t('pages.inbounds.form.anytlsForwardHint')}
      >
        <Input allowClear placeholder="http://127.0.0.1" />
      </FormField>
      <FormField
        name={['settings', 'paddingScheme']}
        label={t('pages.inbounds.form.anytlsPaddingScheme')}
        tooltip={t('pages.inbounds.form.anytlsPaddingSchemeHint')}
      >
        <Input allowClear placeholder="/etc/anytls/padding.txt" />
      </FormField>
      <FormField
        name={['settings', 'debug']}
        label={t('pages.inbounds.form.anytlsDebug')}
        valueProp="checked"
      >
        <Switch />
      </FormField>
      <FormField
        name={['settings', 'routeThroughXray']}
        label={t('pages.inbounds.form.anytlsRouteThroughXray')}
        tooltip={t('pages.inbounds.form.anytlsRouteThroughXrayHint')}
        valueProp="checked"
      >
        <Switch />
      </FormField>
      {routeThroughXray && (
        <FormField
          name={['settings', 'outboundTag']}
          label={t('pages.inbounds.form.mtgRouteOutbound')}
          tooltip={t('pages.inbounds.form.mtgRouteOutboundHint')}
        >
          <Select
            allowClear
            showSearch
            placeholder={t('pages.inbounds.form.mtgRouteOutboundPlaceholder')}
            options={(outboundTags ?? []).map((tag) => ({ value: tag, label: tag }))}
          />
        </FormField>
      )}
    </>
  );
}
