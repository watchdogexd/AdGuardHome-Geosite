import React, { useState } from 'react';
import { useTranslation } from 'react-i18next';
import clsx from 'clsx';
import apiClient from '../../../../api/Api';

const GeositeTest = () => {
    const { t } = useTranslation();
    const [domain, setDomain] = useState('');
    const [result, setResult] = useState<any>(null);
    const [error, setError] = useState('');
    const [testing, setTesting] = useState(false);

    const handleTest = async () => {
        if (!domain.trim()) {
            setError(t('geosite_test_domain_required'));
            return;
        }

        setTesting(true);
        setError('');
        setResult(null);

        try {
            const response = await apiClient.testGeosite(domain.trim());
            setResult(response);
        } catch (err: any) {
            setError(err.message || t('geosite_test_error'));
        } finally {
            setTesting(false);
        }
    };

    const handleKeyPress = (e: React.KeyboardEvent) => {
        if (e.key === 'Enter') {
            handleTest();
        }
    };

    return (
        <div className="form__group">
            <label className="form__label form__label--with-desc">
                {t('geosite_test_title')}
            </label>
            <div className="form__desc form__desc--top">
                {t('geosite_test_desc')}
            </div>

            <div className="input-group mb-2">
                <input
                    type="text"
                    className="form-control"
                    placeholder={t('geosite_test_placeholder')}
                    value={domain}
                    onChange={(e) => setDomain(e.target.value)}
                    onKeyPress={handleKeyPress}
                    disabled={testing}
                />
                <div className="input-group-append">
                    <button
                        type="button"
                        className={clsx('btn btn-primary', {
                            'btn-loading': testing,
                        })}
                        onClick={handleTest}
                        disabled={testing || !domain.trim()}>
                        {t('geosite_test_button')}
                    </button>
                </div>
            </div>

            {error && (
                <div className="alert alert-danger mt-2" role="alert">
                    {error}
                </div>
            )}

            {result && (
                <div className="alert alert-info mt-2" role="alert">
                    <div>
                        <strong>{t('geosite_test_result_domain')}:</strong> {result.domain}
                    </div>
                    {result.categories && result.categories.length > 0 && (
                        <div className="mt-2">
                            <strong>{t('geosite_test_result_categories')}:</strong>
                            <div className="mt-1">
                                {result.categories.join(', ')}
                            </div>
                        </div>
                    )}
                    {result.message && (
                        <div className="mt-2">
                            <em>{result.message}</em>
                        </div>
                    )}
                </div>
            )}
        </div>
    );
};

export default GeositeTest;
