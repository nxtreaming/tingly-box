import PlatformBotPage from './PlatformBotPage';
import { usePlatformGuide } from '@/constants/platformGuides';

const QQPage = () => {
    const config = usePlatformGuide('qq');

    return (
        <PlatformBotPage
            platformId="qq"
            platformName={config?.name || 'QQ'}
            platformGuide={config?.guide}
        />
    );
};

export default QQPage;
