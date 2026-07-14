import PlatformBotPage from './PlatformBotPage';
import { usePlatformGuide } from '@/constants/platformGuides';

const TelegramPage = () => {
    const config = usePlatformGuide('telegram');

    return (
        <PlatformBotPage
            platformId="telegram"
            platformName={config?.name || 'Telegram'}
            platformGuide={config?.guide}
        />
    );
};

export default TelegramPage;
