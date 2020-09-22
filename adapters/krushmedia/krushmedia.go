package krushmedia

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"text/template"

	"github.com/mxmCherry/openrtb"
	"github.com/prebid/prebid-server/adapters"
	"github.com/prebid/prebid-server/errortypes"
	"github.com/prebid/prebid-server/macros"
	"github.com/prebid/prebid-server/openrtb_ext"
)

type KrushmediaAdapter struct {
	endpoint template.Template
}

func NewKrushmediaBidder(endpointTemplate string) *KrushmediaAdapter {
	template, err := template.New("endpointTemplate").Parse(endpointTemplate)
	if err != nil {
		return nil
	}
	return &KrushmediaAdapter{endpoint: *template}
}

func checkHasImps(request *openrtb.BidRequest) error {
	if len(request.Imp) == 0 {
		err := &errortypes.BadInput{
			Message: "Missing Imp Object",
		}
		return err
	}
	return nil
}

func getHeaders(request *openrtb.BidRequest) *http.Header {
	headers := http.Header{}
	headers.Add("Content-Type", "application/json;charset=utf-8")
	headers.Add("Accept", "application/json")
	headers.Add("X-Openrtb-Version", "2.5")

	if request.Device != nil {
		if len(request.Device.UA) > 0 {
			headers.Add("User-Agent", request.Device.UA)
		}

		if len(request.Device.IP) > 0 {
			headers.Add("X-Forwarded-For", request.Device.IP)
		}

		if len(request.Device.Language) > 0 {
			headers.Add("Accept-Language", request.Device.Language)
		}

		if request.Device.DNT != nil {
			headers.Add("Dnt", strconv.Itoa(int(*request.Device.DNT)))
		}
	}

	return &headers
}

func (a *KrushmediaAdapter) MakeRequests(
	openRTBRequest *openrtb.BidRequest,
	reqInfo *adapters.ExtraRequestInfo,
) (
	requestsToBidder []*adapters.RequestData,
	errs []error,
) {

	request := *openRTBRequest

	if err := checkHasImps(&request); err != nil {
		return nil, []error{err}
	}

	var errors []error
	var krushmediaExt *openrtb_ext.ExtKrushmedia
	var err error

	for i, imp := range request.Imp {
		krushmediaExt, err = a.getImpressionExt(&imp)
		if err != nil {
			errors = append(errors, err)
			break
		}
		request.Imp[i].Ext = nil
	}

	if len(errors) > 0 {
		return nil, errors
	}

	url, err := a.buildEndpointURL(krushmediaExt)
	if err != nil {
		return nil, []error{err}
	}

	reqJSON, err := json.Marshal(request)
	if err != nil {
		return nil, []error{err}
	}

	return []*adapters.RequestData{{
		Method:  http.MethodPost,
		Body:    reqJSON,
		Uri:     url,
		Headers: *getHeaders(&request),
	}}, nil
}

func (a *KrushmediaAdapter) getImpressionExt(imp *openrtb.Imp) (*openrtb_ext.ExtKrushmedia, error) {
	var bidderExt adapters.ExtImpBidder
	if err := json.Unmarshal(imp.Ext, &bidderExt); err != nil {
		return nil, &errortypes.BadInput{
			Message: "Bidder extension not provided or can't be unmarshalled",
		}
	}
	var krushmediaExt openrtb_ext.ExtKrushmedia
	if err := json.Unmarshal(bidderExt.Bidder, &krushmediaExt); err != nil {
		return nil, &errortypes.BadInput{
			Message: "Error while unmarshaling bidder extension",
		}
	}
	return &krushmediaExt, nil
}

func (a *KrushmediaAdapter) buildEndpointURL(params *openrtb_ext.ExtKrushmedia) (string, error) {
	endpointParams := macros.EndpointTemplateParams{AccountID: params.AccountID}
	return macros.ResolveMacros(a.endpoint, endpointParams)
}

func (a *KrushmediaAdapter) CheckResponseStatusCodes(response *adapters.ResponseData) error {
	if response.StatusCode == http.StatusNoContent {
		return nil
	}

	if response.StatusCode == http.StatusBadRequest {
		return &errortypes.BadInput{
			Message: fmt.Sprintf("Unexpected status code: [ %d ]", response.StatusCode),
		}
	}

	if response.StatusCode == http.StatusServiceUnavailable {
		return nil
	}

	if response.StatusCode != http.StatusOK {
		return &errortypes.BadInput{
			Message: fmt.Sprintf("Something went wrong, please contact your Account Manager. Status Code: [ %d ] ", response.StatusCode),
		}
	}

	return nil
}

func (a *KrushmediaAdapter) MakeBids(
	openRTBRequest *openrtb.BidRequest,
	requestToBidder *adapters.RequestData,
	bidderRawResponse *adapters.ResponseData,
) (
	bidderResponse *adapters.BidderResponse,
	errs []error,
) {
	httpStatusError := a.CheckResponseStatusCodes(bidderRawResponse)
	if httpStatusError != nil {
		return nil, []error{httpStatusError}
	}

	responseBody := bidderRawResponse.Body
	var bidResp openrtb.BidResponse
	if err := json.Unmarshal(responseBody, &bidResp); err != nil {
		return nil, []error{&errortypes.BadServerResponse{
			Message: "Bad Server Response",
		}}
	}

	bidResponse := adapters.NewBidderResponseWithBidsCapacity(len(bidResp.SeatBid[0].Bid))
	sb := bidResp.SeatBid[0]

	for _, bid := range sb.Bid {
		bidResponse.Bids = append(bidResponse.Bids, &adapters.TypedBid{
			Bid:     &bid,
			BidType: getMediaTypeForImp(bid.ImpID, openRTBRequest.Imp),
		})
	}
	return bidResponse, nil
}

func getMediaTypeForImp(impId string, imps []openrtb.Imp) openrtb_ext.BidType {
	mediaType := openrtb_ext.BidTypeBanner
	for _, imp := range imps {
		if imp.ID == impId {
			if imp.Video != nil {
				mediaType = openrtb_ext.BidTypeVideo
			} else if imp.Native != nil {
				mediaType = openrtb_ext.BidTypeNative
			}
			return mediaType
		}
	}
	return mediaType
}