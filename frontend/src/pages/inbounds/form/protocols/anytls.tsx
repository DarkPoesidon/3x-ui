import { useTranslation } from 'react-i18next';
import { Alert, Button, Input, Select, Switch } from 'antd';
import { useFormContext, useWatch } from 'react-hook-form';

import { FormField } from '@/components/form/rhf';
import { useOutboundTags } from '@/api/queries/useOutboundTags';
import { ANYTLS_DEFAULT_FORWARD } from '@/lib/xray/inbound-defaults';
import { RandomUtil } from '@/utils';

export default function AnytlsFields() {
  const { t } = useTranslation();
  const { control, setValue } = useFormContext();
  const routeThroughXray = useWatch({ control, name: 'settings.routeThroughXray' }) as
    | boolean
    | undefined;
  const paddingMode = useWatch({ control, name: 'settings.paddingMode' }) as string | undefined;
  const certFile = useWatch({ control, name: 'settings.certFile' }) as string | undefined;
  const { data: outboundTags } = useOutboundTags();

  // Fills everything an inbound needs to work without a domain. The fields stay
  // visible and editable afterwards: with AnyTLS a wrong value fails in ways
  // that are hard to trace, so seeing what was set is worth more than hiding it.
  const applyNoDomainDefaults = () => {
    const opts = { shouldDirty: true, shouldValidate: true } as const;
    setValue('port', 443, opts);
    setValue('settings.sni', `cdn.${RandomUtil.randomLowerAndNum(10)}.com`, opts);
    setValue('settings.certFile', '', opts);
    setValue('settings.keyFile', '', opts);
    setValue('settings.forward', ANYTLS_DEFAULT_FORWARD, opts);
    setValue('settings.paddingMode', 'strong', opts);
    setValue('settings.routeThroughXray', true, opts);
  };

  return (
    <>
      <Alert
        type="info"
        showIcon
        style={{ marginBottom: 16 }}
        message={t('pages.inbounds.form.anytlsQuickSetup')}
        description={t('pages.inbounds.form.anytlsQuickSetupHint')}
        action={
          <Button size="small" onClick={applyNoDomainDefaults}>
            {t('pages.inbounds.form.anytlsQuickSetupApply')}
          </Button>
        }
      />
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
      {!certFile && (
        <Alert
          type="warning"
          showIcon
          style={{ marginBottom: 16 }}
          message={t('pages.inbounds.form.anytlsSelfSigned')}
        />
      )}
      <FormField
        name={['settings', 'forward']}
        label={t('pages.inbounds.form.anytlsForward')}
        tooltip={t('pages.inbounds.form.anytlsForwardHint')}
      >
        <Input allowClear placeholder={ANYTLS_DEFAULT_FORWARD} />
      </FormField>
      <FormField
        name={['settings', 'paddingMode']}
        label={t('pages.inbounds.form.anytlsPaddingMode')}
        tooltip={t('pages.inbounds.form.anytlsPaddingModeHint')}
      >
        <Select
          options={[
            { value: 'strong', label: t('pages.inbounds.form.anytlsPaddingStrong') },
            { value: 'default', label: t('pages.inbounds.form.anytlsPaddingDefault') },
            { value: 'custom', label: t('pages.inbounds.form.anytlsPaddingCustom') },
            { value: 'file', label: t('pages.inbounds.form.anytlsPaddingFile') },
          ]}
        />
      </FormField>
      {paddingMode === 'custom' && (
        <FormField
          name={['settings', 'paddingSchemeText']}
          label={t('pages.inbounds.form.anytlsPaddingSchemeText')}
          tooltip={t('pages.inbounds.form.anytlsPaddingSchemeTextHint')}
        >
          <Input.TextArea rows={8} spellCheck={false} placeholder={'stop=8\n0=30-30\n1=100-400'} />
        </FormField>
      )}
      {paddingMode === 'file' && (
        <FormField
          name={['settings', 'paddingScheme']}
          label={t('pages.inbounds.form.anytlsPaddingScheme')}
          tooltip={t('pages.inbounds.form.anytlsPaddingSchemeHint')}
        >
          <Input allowClear placeholder="/etc/x-ui/anytls/padding.txt" />
        </FormField>
      )}
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
